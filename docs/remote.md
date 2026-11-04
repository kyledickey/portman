# Use portman across machines

Your apps might run on one machine while your browser runs on another. A VM, a dev server, and a second laptop all fit this case. Two portman installs connect them. The machine with the apps shares its port. The machine with the browser forwards unknown names to it.

```text
browser ── http://api.localhost ──▶ portman on your laptop
                                      │ no local app named api
                                      ▼
                                    portman on devbox.lan ──▶ localhost:8080
```

The `Host` header passes through unchanged, so the remote portman routes `api.localhost` the same way it would for a local browser.

## Set up the machine that runs the apps

1. Install portman and start it:

	```sh
	pm start
	```

2. Run `pm config` and turn on **Share with other machines**. portman now listens on every network interface instead of only `127.0.0.1`.

3. If the machine has a firewall, open TCP port 80. On Ubuntu with ufw, run:

	```sh
	sudo ufw allow 80/tcp
	```

4. Add your apps as usual:

	```sh
	pm add api 8080
	```

Sharing exposes every mapped app to any machine that can reach port 80. Turn it on only for networks you trust.

## Set up the machine with the browser

1. Install portman and start it. On Windows, run this from an Administrator terminal:

	```sh
	pm start
	```

2. Forward names to the other machine:

	```sh
	pm forward devbox.lan
	```

3. Run `pm list` to check the connection. The last line shows the forward target with `● up` when it answers.

Open `http://api.localhost`. You see the app from `devbox.lan`.

Apps you add on this machine take priority. If both machines have an app named `api`, the local one wins. Run `pm forward --off` to stop forwarding.

## Use a host name, not an IP address

portman looks up the forward host each time it connects. If you forward to a host name, an IP address change on the other machine needs no action. If you forward to an IP address, run `pm forward` again after the address changes.

## Example: a Hyper-V VM on Windows

A VM on the Hyper-V **Default Switch** gets a new IP address range after the Windows host restarts. Windows registers each VM as `<vm-hostname>.mshome.net`, and that name follows the VM to its new address.

1. In the VM, install portman, turn on sharing with `pm config`, and add apps with `pm add`.
2. In the VM, run `hostname` to get its name, such as `my-ubuntu-vm`.
3. On Windows, in an Administrator terminal, run `pm start`.
4. On Windows, run `pm forward my-ubuntu-vm.mshome.net`.

If `pm list` on Windows shows the forward target as down, check that Windows resolves the name with `ping my-ubuntu-vm.mshome.net`. If it doesn't resolve, forward to the VM's IP address instead. Get the address by running `hostname -I` in the VM.
