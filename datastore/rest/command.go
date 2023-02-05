package rest

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

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

type CommandHandler interface {
	Name() string
	Fields() []string

	NewInstance() interface{}
	NewInstanceFromValues(Values) (interface{}, error)
}

type restCommand struct {
	util.ClientTLSFlags

	impl   CommandHandler
	verb   string
	fields []string

	url string
}

func newRestCommand(verb string, impl CommandHandler) *restCommand {
	return &restCommand{
		impl:   impl,
		verb:   verb,
		fields: impl.Fields(),
	}
}

func (r *restCommand) Name() string {
	return fmt.Sprintf("%s-%s", r.verb, r.impl.Name())
}

func (r *restCommand) Synopsis() string {
	return fmt.Sprintf("%s a %s object", strings.Title(r.verb), r.impl.Name())
}

func (r *restCommand) Usage() string {
	return r.Synopsis() + ".\n\n"
}

func (r *restCommand) SetFlags(f *flag.FlagSet) {
	f.StringVar(&r.url, "url", "", "API server `URL`")
	r.ClientTLSFlags.SetFlags(f)
}

func (r *restCommand) client() (Client, error) {
	tlsConf, err := r.TLSClientConfig()
	if err != nil {
		return nil, err
	}
	return newClient(r.url, r.impl.Name(), tlsConf), nil
}

type restCommandWithFlags struct {
	*restCommand

	fieldValues Values
}

func newRestCommandWithFlags(verb string, impl CommandHandler) *restCommandWithFlags {
	return &restCommandWithFlags{restCommand: newRestCommand(verb, impl)}
}

func (r *restCommandWithFlags) SetFlags(f *flag.FlagSet) {
	r.restCommand.SetFlags(f)
	r.fieldValues = make(Values)
	for _, field := range r.fields {
		s := r.fieldValues.add(field)
		f.StringVar(s, field, "", "set this field")
	}
}

type restAddCommand struct {
	*restCommandWithFlags
}

func newAddCommand(impl CommandHandler) *restAddCommand {
	return &restAddCommand{newRestCommandWithFlags("create", impl)}
}

func (c *restAddCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	obj, err := c.impl.NewInstanceFromValues(c.fieldValues)
	if err != nil {
		return fatalErr(err)
	}
	client, err := c.client()
	if err != nil {
		return fatalErr(err)
	}
	return fatalErr(client.Add(ctx, obj))
}

type restUpdateCommand struct {
	*restCommandWithFlags
}

func newUpdateCommand(impl CommandHandler) *restUpdateCommand {
	return &restUpdateCommand{newRestCommandWithFlags("set", impl)}
}

func (c *restUpdateCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	obj, err := c.impl.NewInstanceFromValues(c.fieldValues)
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
	*restCommand
}

func newDeleteCommand(impl CommandHandler) *restDeleteCommand {
	return &restDeleteCommand{newRestCommand("delete", impl)}
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
	*restCommand
}

func newGetCommand(impl CommandHandler) *restGetCommand {
	return &restGetCommand{newRestCommand("get", impl)}
}

func (c *restGetCommand) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if f.NArg() != 1 {
		return syntaxErr("wrong number of arguments")
	}
	client, err := c.client()
	if err != nil {
		return fatalErr(err)
	}

	obj := c.impl.NewInstance()
	if err := client.Find(ctx, f.Arg(0), obj); err != nil {
		return fatalErr(err)
	}

	return fatalErr(json.NewEncoder(os.Stdout).Encode(obj))
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

func RegisterCommands(impl CommandHandler) {
	section := fmt.Sprintf("managing '%s' objects", impl.Name())
	subcommands.Register(newAddCommand(impl), section)
	subcommands.Register(newUpdateCommand(impl), section)
	subcommands.Register(newDeleteCommand(impl), section)
	subcommands.Register(newGetCommand(impl), section)
}
