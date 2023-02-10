package crud

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"git.autistici.org/ai3/attic/wig/datastore/crud/httptransport"
	"git.autistici.org/ai3/attic/wig/util"
	"github.com/google/subcommands"
)

type Values map[string]*string

func (v Values) add(k string) *string {
	s := new(string)
	v[k] = s
	return s
}

func (v Values) Get(k string) string {
	if s, ok := v[k]; ok {
		return *s
	}
	return ""
}

type command struct {
	util.ClientCommand

	m    *Model
	t    TypeMeta
	verb string

	// This is set by the --url flag.
	urlFlag string

	// This is the root path of the API endpoint.
	urlPrefix string
}

func newRestCommand(m *Model, t TypeMeta, url, verb string) *command {
	return &command{
		m:         m,
		t:         t,
		urlPrefix: url,
		verb:      verb,
	}
}

func (r *command) Name() string {
	return fmt.Sprintf("%s-%s", r.verb, r.t.Name())
}

func (r *command) Synopsis() string {
	// nolint: staticcheck
	return fmt.Sprintf("%s a %s object", strings.Title(r.verb), r.t.Name())
}

func (r *command) Usage() string {
	return r.Synopsis() + ".\n\n"
}

func (r *command) SetFlags(f *flag.FlagSet) {
	f.StringVar(&r.urlFlag, "url", util.FlagDefault("url", ""), "API server `URL`")
	r.ClientCommand.SetFlags(f)
}

func (r *command) client() (API, error) {
	if r.urlFlag == "" {
		return nil, errors.New("must specify --url")
	}
	client, err := r.HTTPClient()
	if err != nil {
		return nil, err
	}
	url := httptransport.JoinURL(r.urlFlag, r.urlPrefix)
	return r.m.Client(url, client).Get(r.t.Name()), nil
}

type commandWithFlags struct {
	*command

	fieldValues Values
}

func newRestCommandWithFlags(m *Model, t TypeMeta, url, verb string) *commandWithFlags {
	return &commandWithFlags{command: newRestCommand(m, t, url, verb)}
}

func (r *commandWithFlags) SetFlags(f *flag.FlagSet) {
	r.command.SetFlags(f)
	r.fieldValues = make(Values)
	for _, field := range r.t.Fields() {
		flagName := strings.Replace(field, "_", "-", -1)
		s := r.fieldValues.add(field)
		f.StringVar(s, flagName, "", "set this field")
	}
}

type restCreateCommand struct {
	*commandWithFlags
}

func newCreateCommand(m *Model, t TypeMeta, url string) *restCreateCommand {
	return &restCreateCommand{newRestCommandWithFlags(m, t, url, "create")}
}

func (c *restCreateCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	obj, err := c.t.NewInstanceFromValues(c.fieldValues)
	if err != nil {
		return fatalErr(err)
	}
	client, err := c.client()
	if err != nil {
		return fatalErr(err)
	}
	return fatalErr(client.Create(ctx, obj))
}

type restUpdateCommand struct {
	*commandWithFlags
}

func newUpdateCommand(m *Model, t TypeMeta, url string) *restUpdateCommand {
	return &restUpdateCommand{newRestCommandWithFlags(m, t, url, "update")}
}

func (c *restUpdateCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	obj, err := c.t.NewInstanceFromValues(c.fieldValues)
	if err != nil {
		return fatalErr(err)
	}
	client, err := c.client()
	if err != nil {
		return fatalErr(err)
	}
	return fatalErr(client.Update(ctx, obj))
}

type restDeleteCommand struct {
	*command
}

func newDeleteCommand(m *Model, t TypeMeta, url string) *restDeleteCommand {
	return &restDeleteCommand{newRestCommand(m, t, url, "delete")}
}

func (c *restDeleteCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if f.NArg() != 1 {
		return syntaxErr("wrong number of arguments")
	}
	client, err := c.client()
	if err != nil {
		return fatalErr(err)
	}
	return fatalErr(client.Delete(ctx, f.Arg(0)))
}

type restGetCommand struct {
	*command
}

func newGetCommand(m *Model, t TypeMeta, url string) *restGetCommand {
	return &restGetCommand{newRestCommand(m, t, url, "get")}
}

func (c *restGetCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if f.NArg() != 1 {
		return syntaxErr("wrong number of arguments")
	}
	client, err := c.client()
	if err != nil {
		return fatalErr(err)
	}

	query := map[string]string{
		c.t.PrimaryKeyField(): f.Arg(0),
	}

	// Type is embedded in the Client so we don't need to specify it.
	return fatalErr(client.Find(ctx, "", query, func(obj interface{}) error {
		if err := json.NewEncoder(os.Stdout).Encode(obj); err != nil {
			return err
		}
		fmt.Printf("\n")
		return nil
	}))
}

type restFindCommand struct {
	*command
}

func newFindCommand(m *Model, t TypeMeta, url string) *restFindCommand {
	return &restFindCommand{newRestCommand(m, t, url, "find")}
}

func (c *restFindCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	client, err := c.client()
	if err != nil {
		return fatalErr(err)
	}

	query := make(map[string]string)
	for _, arg := range f.Args() {
		parts := strings.SplitN(arg, "=", 1)
		if len(parts) != 2 {
			return fatalErr(errors.New("could not parse query as attr=value"))
		}
		query[parts[0]] = parts[1]
	}

	// Type is embedded in the Client so we don't need to specify it.
	i := 0
	if err := client.Find(ctx, "", query, func(obj interface{}) error {
		if i == 0 {
			fmt.Printf("[")
		} else {
			fmt.Printf(", ")
		}
		i++
		return json.NewEncoder(os.Stdout).Encode(obj)
	}); err != nil {
		return fatalErr(err)
	}
	fmt.Printf("]\n")
	return subcommands.ExitSuccess
}

func syntaxErr(msg string) subcommands.ExitStatus {
	log.Printf("invocation error: %s", msg)
	return subcommands.ExitUsageError
}

func fatalErr(err error) subcommands.ExitStatus {
	if err != nil {
		log.Printf("fatal error: %v", err)
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

func RegisterCommands(m *Model, t TypeMeta, url string) {
	section := fmt.Sprintf("managing '%s' objects", t.Name())
	subcommands.Register(newCreateCommand(m, t, url), section)
	subcommands.Register(newUpdateCommand(m, t, url), section)
	subcommands.Register(newDeleteCommand(m, t, url), section)
	subcommands.Register(newGetCommand(m, t, url), section)
	subcommands.Register(newFindCommand(m, t, url), section)
}
