This directory contains the tests that examine Virtlet networking
by running a DHCP client and sending actual network packets.

Note that before we switch to Go 1.10, there may be test flakes
because some goroutines may inherit a network namespace from other
goroutines unintentionally. Running at least some of the network tests
separately may help reduce the impact of this problem. For more info,
see containernetworking/cni#262, vishvananda/netns#17, golang/go#20676
