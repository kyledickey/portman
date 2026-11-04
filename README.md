# portman

portman gives the apps you run locally names like `http://api.localhost` instead of `http://localhost:8080`. You map a name to a port once, and every subdomain of that name works too.

```console
$ pm add api 8080
  ✓ http://api.localhost → localhost:8080

$ pm list

  ● portman v0.1.0 on 127.0.0.1:80

  APP   URL                    TARGET
  api   http://api.localhost   localhost:8080   ● up
  web   http://web.localhost   localhost:3000   ○ down
```

portman is one binary, `pm`, with [Caddy](https://caddyserver.com) built in. It runs on Linux, macOS, and Windows.

## Install

On Linux or macOS, run the install script. It downloads the newest stable release, checks its checksum, and puts `pm` in `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/kdicusb/portman/main/install.sh | sh
```

To change what it installs, set these variables before `sh`:

| Variable | Effect |
| --- | --- |
| `PM_INSTALL_DIR` | The directory for `pm`. The default is `/usr/local/bin`. |
| `PM_VERSION` | A release tag to install, such as `v0.2.0-beta.1`. The default is the newest stable release. |
| `PM_FROM` | A directory holding `make dist` output to install from, instead of GitHub. |

If you have Go, you can install with Go instead:

```sh
go install github.com/kdicusb/portman/cmd/pm@latest
```

On Windows, download `pm_<version>_windows_amd64.exe` from the [releases page](https://github.com/kdicusb/portman/releases), rename it to `pm.exe`, and put it in a folder on your `PATH`.

Then start portman:

```sh
pm start
```

`pm start` runs portman in the background as an OS service, so it also starts at boot and restarts if it crashes. On Linux, it asks for your sudo password. On Windows, run it from an Administrator terminal. `pm stop`, `pm update`, and `pm serve` on Linux can ask for sudo too. No other command needs elevated rights.

## Map your apps

```sh
pm add api 8080              # http://api.localhost → localhost:8080
pm add db 10.0.0.5:5432      # any host:port works
pm list                      # every app, and whether its port answers
pm delete api                # or: pm rm api
```

Each app also answers on every name under it. `http://v2.api.localhost` and `http://tenant1.eu.api.localhost` both reach `api`, so apps that route by subdomain work with no extra setup.

A bare port such as `8080` means `localhost:8080`, which reaches apps that listen on `127.0.0.1` and apps that listen on `::1`.

Changes apply right away. The running service watches its config file and reloads within a fraction of a second.

### Why `.localhost`

Browsers resolve every `*.localhost` name to your own machine without any DNS setup. So do `curl` and systemd-resolved. That gives portman wildcard names on every OS with nothing to configure.

To use another suffix, such as `.test`, set it with `pm config`. You then need DNS that sends `*.test` to the portman machine. Note, `.local` probably won't work.

## Use apps on another machine

Your apps might run on a VM, a WSL distro, or a dev server while you browse from your laptop. Install portman on both machines and point one at the other:

```sh
# On the machine that runs the apps
pm config                      # turn on "Share with other machines"

# On the machine with the browser
pm forward devbox.lan          # the other machine's host name or IP
```

Now `http://api.localhost` on your laptop reaches `api` on `devbox.lan`. Names you add on the laptop still go to the laptop's own apps first. [Use portman across machines](docs/remote.md) covers firewalls, changing IP addresses, and a Hyper-V example.

## Commands

| Command                  | What it does                                                                                                                                       |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pm add NAME PORT`       | Map `NAME.localhost` and its subdomains to `PORT` or `HOST:PORT`. Adding an existing name replaces its target.                                     |
| `pm delete NAME`         | Remove an app. The alias is `pm rm`.                                                                                                               |
| `pm list`                | Show whether portman runs, and each app's URL, target, and status. The alias is `pm ls`.                                                           |
| `pm forward HOST[:PORT]` | Send `*.localhost` names with no local app to portman on `HOST`. The port defaults to 80. `pm forward --off` stops forwarding.                     |
| `pm config`              | Edit settings in a terminal form. Each setting saves when you move off it.                                                                         |
| `pm start`               | Run portman in the background as an OS service, now and at every boot. Run it again to restart portman.                                            |
| `pm stop`                | Stop portman and remove the service. Your config stays.                                                                                            |
| `pm serve`               | Run portman in the foreground and print its log to the terminal. On Linux, if the port needs extra rights, it asks for sudo and still runs as you. |
| `pm logs`                | Show the newest 40 lines of the log. `-n 100` shows more, and `-f` keeps printing new lines until you press Ctrl+C.                                |
| `pm update`              | Replace `pm` with the newest stable release and restart the service. `pm update --beta` includes beta releases.                                    |

Every command accepts `--home DIR`, or the `PORTMAN_HOME` environment variable, to use a different directory for config, logs, and data.

## Configuration

`pm config` edits these settings in a short form. Press Esc when you're done. You can also edit `config.toml` by hand. If a hand edit doesn't parse, portman logs the error and keeps the previous routes.

```toml
domain = 'localhost'   # apps live at NAME.domain
port = 80              # where portman listens
share = false          # true listens on every interface, not only 127.0.0.1
forward = ''           # host:port of another portman for unknown names
log_level = 'info'     # debug, info, warn, or error

[apps]
api = 'localhost:8080'
web = 'localhost:3000'
```

portman keeps all of its files in one directory:

| OS      | Directory                                          |
| ------- | -------------------------------------------------- |
| Linux   | `~/.config/portman`, or `$XDG_CONFIG_HOME/portman` |
| macOS   | `~/Library/Application Support/portman`            |
| Windows | `%AppData%\portman`                                |

| File          | Contents                                                                     |
| ------------- | ---------------------------------------------------------------------------- |
| `config.toml` | Settings and apps.                                                           |
| `pm.log`      | JSON logs from portman and Caddy. It rotates at 10 MB and keeps 3 old files. |
| `caddy/`      | Caddy's internal state.                                                      |

## Troubleshooting

| Symptom                                                      | Fix                                                                                                                          |
| ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| `pm list` says portman isn't running.                        | Run `pm start`. If it still isn't running, run `pm logs` to see why.                                                         |
| `pm list` says another program is using port 80.             | Stop that program, or choose another port in `pm config`. URLs then include the port, such as `http://api.localhost:8080`.   |
| `bind: permission denied`                                    | Ports below 1024 need extra rights. `pm start` grants them to the service. On Linux, `pm serve` asks for sudo to grant them. |
| `portman is already running`                                 | Another portman holds the port. Watch it with `pm logs -f`, or stop it with `pm stop`.                                       |
| The browser shows "isn't responding".                        | portman matched the name, but nothing answered on the app's port. Start the app, or check the port with `pm list`.           |
| The browser shows "isn't mapped".                            | No app has that name. Run `pm list` to see the names that exist.                                                             |
| A dev server rejects the request with "Invalid Host header". | Allow the host in the dev server's config. For Vite, set `server.allowedHosts` to `['.localhost']`.                          |

`pm logs` prints the log as readable text. The raw JSON is in `pm.log`, in the directory listed under [Configuration](#configuration). At `log_level = 'info'` and above, the log leaves out Caddy's startup messages. Set `debug` to see everything, including each request that fails to reach an app.

## Develop

```sh
make build     # bin/pm
make test      # Go tests and release-script tests
make ci        # what CI runs
```

Releases come from tags. `make release-beta` tags and pushes the next beta, such as `v0.2.0-beta.3`. `make release-stable` promotes the newest beta to a stable tag, such as `v0.2.0`. A pushed tag runs `.github/workflows/release.yml`, which builds every platform and publishes a GitHub release. Betas publish as prereleases.

`scripts/release.sh` needs one existing tag to count from. For the first release, run `git tag v0.1.0-beta.1 && git push origin v0.1.0-beta.1`.

## License

[MIT](LICENSE)
