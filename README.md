Wireguard VPN server
===

Simple and minimal multi-gateway VPN service with separate control
plane with separate users.

## How to test

Requires Go 1.17 or newer and Ansible.

```
$ go build ./cmd/wig
$ cd test
$ go run driver.go setup.yml
```

The *driver.go* tool supports a bunch of options to select between
different VM providers (Vagrant / vmine).

