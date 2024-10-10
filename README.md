# glog

Logging solution built with simplicity in mind! Setup your own central logging server easily!
Written solenly in Go with two external packages: viper and fsnotify.

Supported architectures: x86_64, i136, i686, aarch64 and arm*.

Supported Linux distributions: ubuntu, debian, linuxmint, fedora, centos, rhel, rocky, almalinux, arch and manjaroo. 

# Why use glog?

1. Simple configuration - no need to dive deep into vast documentation, just a couple of config variables!
2. Deploy in seconds - install logging server and client, specify desired files and watch your logs fly!
3. If you just need to stream your log files as they are there is no easier way!

# How to use?

1. Download installation script and run it (sudo privileges required):
```bash
bash <(curl -sSLf https://glog.proxilius.eu/download/install.sh)
```
2. Edit configuration file in `/etc/glog/config.toml` as it goes:

3. Run glog service:
```bash
systemctl start glog
```

For more info about configuration see [Configuration](https://github.com/Fishmansky/glog?tab=readme-ov-file#Configuration) section.

# How it works?

When installed and started glog server starts listening on address specified in configuration variable `addr`.

Each glog client watches files that you specified in configuration in `logfiles` section, then sends them to glog logging server specified in `addr`.

Server stores logs from clients in separate directories in `/var/log/glog/[CLIENT_NAME]`. Log files are stored in files named as configured by client

# Plans 

[ ] SSL/TLS support
[ ] ???

# Configuration

There are only 4 main variables: 
- `name` (string) - name of your glog instance, e.g.:
```toml
name="MyServer"
```
- `mode` (string) - running mode, `server` or `client`, e.g.:
```toml
mode="server"
```
- `addr` (string) - in `server` mode it specifies the listening address of glog server; in `client` mode thats the server address this client will connect to, e.g.:
```toml
addr="0.0.0.0:12345"
```
- `debug` (bool)  - enables debug mode, additional debug info will be printed in `/var/log/glog/glog.log`, e.g.:
```toml
debug=false
```

Client only variable:
- `logfiles` (map) - specify the final log name as key and path to log file to be watched as variable, e.g.:
```toml
[logfiles]
"syslog"="/var/log/syslog"
```

Here's an example of server configuration:
```toml
name="MyServer"
mode="server"
addr="0.0.0.0:12345"
debug=false
```

Client configuration example:
```toml
name="MyClient"
mode="client"
addr="123.123.123.123:12345"
debug=false
[logfiles]
"syslog"="/var/log/syslog"
"auth.log"="/var/log/auth.log"
"kern.log"="/var/log/kern.log"
```
