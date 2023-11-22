package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brutella/hc"
	"github.com/brutella/hc/accessory"
	"github.com/brutella/hc/characteristic"
	hclog "github.com/brutella/hc/log"
	"github.com/brutella/hc/service"
	"github.com/peterbourgon/ff/v3"
	"github.com/picatz/roku"
)

type Roku struct {
	endpoint   *roku.Endpoint
	deviceInfo *roku.DeviceInfo

	accessory *accessory.Accessory
	tv        *service.Television
	transport hc.Transport
}

type config struct {
	storagePath string
	homekitPIN  string
	debug       bool
}

func main() {
	var cfg config

	fs := flag.NewFlagSet("roku-homekit", flag.ExitOnError)
	fs.StringVar(
		&cfg.storagePath,
		"storage-path",
		filepath.Join(os.Getenv("HOME"), ".homecontrol", "roku"),
		"Storage path for information about the HomeKit accessory",
	)
	fs.StringVar(&cfg.homekitPIN, "homekit-pin", "00102003", "HomeKit pairing PIN")
	fs.BoolVar(&cfg.debug, "debug", false, "Enable debug mode")

	_ = fs.String("config", "", "Config file")

	ff.Parse(fs, os.Args[1:],
		ff.WithEnvVarPrefix("ROKU"),
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.PlainParser),
	)

	if cfg.debug {
		hclog.Debug.Enable()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Println("Searching for Rokus...")
	var rokus []*Roku

	endpoints, err := roku.Find(5)
	if err != nil {
		log.Fatal(err)
	}

	for _, e := range endpoints {
		r, err := setupRoku(&cfg, e)
		if err != nil {
			log.Println(err)
			continue
		}

		rokus = append(rokus, r)
	}

	hc.OnTermination(func() {
		for _, r := range rokus {
			<-r.transport.Stop()
		}
		cancel()
	})

	for _, r := range rokus {
		log.Printf("Starting transport for %q...", r.deviceInfo.UserDeviceName)
		r.start(ctx)
	}

	<-ctx.Done()
	log.Printf("Exiting")
}

func setupRoku(cfg *config, e *roku.Endpoint) (*Roku, error) {
	deviceInfo, err := e.DeviceInfo()
	if err != nil {
		return nil, fmt.Errorf("unable to get device info for %s: %w", e, err)
	}

	// Quotation marks cause problems with adding accessories.
	// https://github.com/brutella/hc/issues/192
	deviceInfo.UserDeviceName = strings.Replace(deviceInfo.UserDeviceName, `"`, "", -1)

	info := accessory.Info{
		Name:             deviceInfo.UserDeviceName,
		Manufacturer:     deviceInfo.VendorName,
		Model:            fmt.Sprintf("%s (%s)", deviceInfo.FriendlyModelName, deviceInfo.ModelNumber),
		FirmwareRevision: fmt.Sprintf("%s-%s", deviceInfo.SoftwareVersion, deviceInfo.SoftwareBuild),
		SerialNumber:     deviceInfo.SerialNumber,
	}

	r := &Roku{
		endpoint:   e,
		deviceInfo: deviceInfo,
		accessory:  accessory.New(info, accessory.TypeTelevision),
		tv:         service.NewTelevision(),
	}

	r.accessory.AddService(r.tv.Service)

	apps, err := e.Apps()
	if err != nil {
		log.Printf("Error getting apps for %q: %v", info.Name, err)
	} else {
		for _, app := range apps {
			r.addApp(app)
		}
	}

	r.accessory.OnIdentify(r.identify)

	r.tv.ConfiguredName.SetValue(r.deviceInfo.UserDeviceName)
	r.tv.SleepDiscoveryMode.SetValue(characteristic.SleepDiscoveryModeAlwaysDiscoverable)

	r.tv.Active.OnValueRemoteGet(r.getActive)
	r.tv.Active.OnValueRemoteUpdate(r.setActive)

	r.tv.ActiveIdentifier.OnValueRemoteGet(r.getActiveIdentifier)
	r.tv.ActiveIdentifier.OnValueRemoteUpdate(r.setActiveIdentifier)

	r.tv.RemoteKey.OnValueRemoteUpdate(r.setRemoteKey)

	hcConfig := hc.Config{
		Pin:         cfg.homekitPIN,
		StoragePath: filepath.Join(cfg.storagePath, deviceInfo.SerialNumber),
	}

	t, err := hc.NewIPTransport(hcConfig, r.accessory)
	if err != nil {
		return nil, fmt.Errorf("error building IP transport for %q: %w", info.Name, err)
	}
	r.transport = t

	return r, nil
}

func (r *Roku) start(ctx context.Context) {
	go r.transport.Start()
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				r.tv.Active.SetValue(r.getActive())
				r.tv.ActiveIdentifier.SetValue(r.getActiveIdentifier())
			}
		}
	}(ctx)
}

func (r *Roku) addApp(app *roku.App) {
	input := service.NewInputSource()

	input.ConfiguredName.SetValue(app.Name)
	input.Name.SetValue(app.Name)
	input.InputSourceType.SetValue(characteristic.InputSourceTypeApplication)
	input.IsConfigured.SetValue(characteristic.IsConfiguredConfigured)

	id, err := strconv.Atoi(app.ID)
	if err == nil {
		input.Identifier.SetValue(id)
	}

	r.accessory.AddService(input.Service)
	r.tv.AddLinkedService(input.Service)
}

func (r *Roku) identify() {
	if err := r.endpoint.FindRemote(); err != nil {
		log.Printf("Unable to find remote for %q: %v", r.deviceInfo.UserDeviceName, err)
	}
}

func (r *Roku) getActive() int {
	var (
		deviceInfo *roku.DeviceInfo
		err        error
	)

	deviceInfo, err = r.endpoint.DeviceInfo()
	if err != nil {
		log.Printf("unable to get device info for %s: %v", r.deviceInfo.UserDeviceName, err)
		deviceInfo = r.deviceInfo // fallback to last known
	}

	if deviceInfo.PowerMode == "PowerOn" {
		return characteristic.ActiveActive
	} else {
		return characteristic.ActiveInactive
	}
}

func (r *Roku) setActive(active int) {
	key := "PowerOn" // roku package doesn't have this, oddly
	if active == characteristic.ActiveInactive {
		key = roku.PowerOffKey
	}

	if err := r.endpoint.Keypress(key); err != nil {
		log.Printf("Keypress %q on %q: %v", key, r.deviceInfo.UserDeviceName, err)
	}
}

func (r *Roku) getActiveIdentifier() int {
	app, err := r.endpoint.ActiveApp()
	if err != nil {
		log.Printf("Couldn't get active app for %q: %v", r.deviceInfo.UserDeviceName, err)
		return 0
	}

	if app.ID == "" {
		return 0
	}

	id, err := strconv.Atoi(app.ID)
	if err != nil {
		log.Printf("Couldn't convert %q to an int: %v", app.ID, err)
		return 0
	}

	return id
}

func (r *Roku) setActiveIdentifier(id int) {
	if err := r.endpoint.LaunchApp(strconv.Itoa(id), nil); err != nil {
		log.Printf("Couldn't launch app ID %d: %v", id, err)
	}
}

var keymap = map[int]string{
	characteristic.RemoteKeyRewind:      roku.RevKey,
	characteristic.RemoteKeyFastForward: roku.FwdKey,
	characteristic.RemoteKeyNextTrack:   roku.FwdKey,
	characteristic.RemoteKeyPrevTrack:   roku.RevKey,
	characteristic.RemoteKeyArrowUp:     roku.UpKey,
	characteristic.RemoteKeyArrowDown:   roku.DownKey,
	characteristic.RemoteKeyArrowLeft:   roku.LeftKey,
	characteristic.RemoteKeyArrowRight:  roku.RightKey,
	characteristic.RemoteKeySelect:      roku.SelectKey,
	characteristic.RemoteKeyBack:        roku.BackKey,
	characteristic.RemoteKeyExit:        roku.HomeKey,
	characteristic.RemoteKeyPlayPause:   roku.PlayKey,
	characteristic.RemoteKeyInfo:        roku.InfoKey,
}

func (r *Roku) setRemoteKey(k int) {
	if key := keymap[k]; key != "" {
		if err := r.endpoint.Keypress(key); err != nil {
			log.Printf("Keypress %q on %q: %v", key, r.deviceInfo.UserDeviceName, err)
		}
	}
}
