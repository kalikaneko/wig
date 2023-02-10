Wireguard VPN server
===

Simple and minimal multi-gateway VPN service with separate control
plane. Meant to support small-scale VPNs as well as multi-homed larger
deployments. The focus is on the Internet routing use case rather than
the "connect groups of devices" scenario (e.g. Tailscale).


## Architecture

Wig consists of a *control plane* which holds the system
configuration, and *gateway nodes* that implement it as network
interface and Wireguard configurations. The control plane is meant to
be out of the serving path and not be a critical dependency (except
when state changes are needed, of course).

The control plane is modeled as a simple data store with a sequential
log of operations, so components can connect to each other via
asynchronous replication. This capability makes it possible to build
custom trees of data propagation, to suit most types of deployment.

*Datastore* and *gateway* components can be connected this
way. Datastore nodes are backed by a SQLite database, and can run in
either "primary" (read-write) or "follower" (read-only) mode. So,
while there can only be one primary datastore, one can implement a
sufficient degree of high availability with additional solutions such
as [Litestream](https://litestream.io/).

The *gateway* component requires full control of the host's network
configuration, either via capabilities or by running as root. There
should be only one gateway running on a host.

### Data model

The data model includes interfaces and peers, which belong to specific
interfaces. Support for multiple interfaces is meant to allow
different outbound network configurations, in a user-visible way
(since one would need to communicate different connection parameters
to users). They behave as entirely separate VPN networks.


### Deployment

Every deployment is going to require at least one datastore and one
gateway component, possibly running on the same host. This is the
minimal viable configuration.

In order to plan more complex deployments, it is useful to have a
mental model of what goes on with the data flow of the control plane:
the first fact to know is that datastore components are *stateful*
(clearly, as the store the data itself) while gateways are
*stateless*. From a data flow perspective, a stateless component must
retrieve the full state at startup.

Deployments running on multiple hosts might want to minimize the
dependency on the primary datastore by running secondary follower
datastore components closer to, or co-located with, each gateway
host. This way, if the primary datastore is unreachable, the gateway
hosts can still be restarted successfully and provide service to the
users.

Wig attempts to be configuration-management-friendly by delegating all
deployment-related configuration to an external CM system: all
connection-related parameters are exposed as configuration flags and
no information on the deployment is kept in the datastore.

### Service-to-service authentication

Service components need to be able to authenticate each other. Wig
supports two main mechanisms:

* "Bearer token" authentication, which is basically HTTP Basic
  authentication with randomly-generated usernames / passwords. This
  works using a table of tokens in the datastore, managed by the
  command-line tool.

* mTLS. In this scenario, the TLS connection will require the usage of
  client certificates. The Common Name (CN) field of the client
  certificate's subject is used as the identity, matched against a
  statically specified allow list.

The control plane API supports a rudimentary
[RBAC](https://en.wikipedia.org/wiki/Role-based_access_control) access
control model, with the following predefined roles:

* *admin* has full access to the API
* *follower* can only access the asynchronous replication API (read-only)
* *registrar* can only access the peer registration API

## Operations

### Installation

The software ships as a single binary with no dependencies. To build
it, from the root of this repository, run:

```shell
$ go build ./cmd/wig
```

This will create a *wig* binary in the current directory.

### Initializing the bearer token authentication subsystem

When using bearer-token authentication mechanism it is necessary to
manually initialize the database with an "admin" token, so that the
command-line tool can perform administrative operations on the
network.

This is done by running, on the primary datastore host, the following
command:

```shell
$ wig init --db=/path/to/your/db.sqlite
{"id":"NVPlVyMU7yRuBFQ8E3JJj7WvnIdDjpac","secret":"ypUKa5dl8fKLkehfCQzECendFTdmnjbt"}
```

The tool will create a new authentication token with *admin* role, and
output the token and its associated secret as JSON.

For better compatibility with configuration management systems, it is
also possible to specify the token ID and secret as command-line
arguments, instead of letting the tool generate them.

### Session identification

The primary datastore component continuously receives statistics from
the gateway nodes. It uses this data to detect *sessions*, that is, to
some approximation, peer connections and disconnections. Since
Wireguard does not have a concept of "connection", this is done by
looking for time intervals when the peer is inactive.

Session logs do not store PII such as the peer's IP address, but they
can be optionally augmented with broad location data (country, ASN) in
order to provide meaningful access logs to users.

This data also allows one to detect abandoned peer definitions that
have not been used in a long time.

### Metrics

The gateway jobs export Prometheus metrics, including per-peer
bandwidth statistics, over a dedicated HTTP port without
authentication.

### Restoring the primary datastore from backup

The asynchronous replication protocol we're using favors overall
system consistency, so if the sequence number of your primary
datastore regresses, as it would happen when restoring an older
backup, the followers would receive a desynchronization error, and
fall back to recovering from a fresh snapshot without any manual
intervention required.

So, in case of a restore of the primary datastore from an (older)
backup, there should be nothing to do except accepting the loss of
data. This is why this system is not a replicated data store, but it's
best to think of it as a *data flow* engine.

## API

The primary datastore offers a HTTP API with some methods that can be
useful for integration.

All HTTP API methods expect to receive POST requests, with an optional
JSON-encoded request body (and a Content-Type of
*application/json*). Responses will be also JSON-encoded.

#### `/api/v1/register-peer`

Request attributes:

* *interface* - Interface name
* *public_key* - Public key of the peer
* *ttl* - TTL in seconds

Create a new peer and allocate free IP addresses for it. The new peer
will get IPv4 / IPv6 addresses depending on the networks defined on
the specified interface.

RBAC target: *register-peer* (included in the default roles *admin*
and *registrar*).

## Command-line tool

The software comes with a command-line tool, *wig*, that can start the
various components and that offers access to administrative
functionality.

The tool talks to the primary datastore server over HTTP, and it is
meant to be used with *admin* authentication credentials. All admin
commands support the *--auth-token* and *--auth-secret* flags to set
these credentials.

The tool offers a series of CRUD data manipulation commands for the
various object types stored in the datastore. These commands will have
an immediate effect on the gateway configuration. Create / update /
delete commands apply to an individual object whose primary key and
attributes can be set via command-line flags. The *get* command
requires an object's primary key as an argument. The *find* command
will instead accept command-line arguments in *attribute=value* form
(including the empty query) and will print all matching objects.

## How to test

Requires Go 1.17 or newer and Ansible.

```
$ go build ./cmd/wig
$ cd test
$ go run driver.go setup.yml
```

The *driver.go* tool supports a bunch of options to select between
different VM providers (Vagrant / vmine).

