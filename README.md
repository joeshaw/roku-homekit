# roku-homekit

HomeKit support for Roku devices using
[hc](https://github.com/brutella/hc) and @picatz's [roku Go
library](https://github.com/picatz/roku).

Newer Roku devices have native support for HomeKit, but this service allows any Roku device (with the External Control Protocol enabled) to be used with HomeKit.

When running, this service publishes a HomeKit accessory for every Roku device it can find on the local network.

Applications installed on the Roku appear as inputs on the HomeKit
device.  However, these inputs are static -- applications that are
installed or removed will not be reflected until roku-homekit is
restarted.  As far as I can tell this seems to be a limitation of
HomeKit.

With this running, you can use Siri to launch apps on your Roku or
control playback, and the remote in the iPhone's control center can
control your Roku.

## Installing

The tool can be installed with:

    go get -u github.com/joeshaw/roku-homekit

Then you can run the service:

    roku-homekit

The service will use SSDP to look for any Roku devices on the local
network for 5 seconds, and then instantiate the HomeKit accessories.

To pair, open up your Home iOS app, click the + icon, choose "Add
Accessory" and then tap "Don't have a Code or Can't Scan?"  You should
see any Rokus under "Nearby Accessories."  Tap that and enter the PIN
00102003 (or whatever you chose on the command-line).

## Contributing

Issues and pull requests are welcome.  When filing a PR, please make
sure the code has been run through `gofmt`.

## License

Copyright 2021 Joe Shaw

`roku-homekit` is licensed under the MIT License.  See the LICENSE
file for details.
