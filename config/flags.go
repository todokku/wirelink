package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/fastcat/wirelink/internal"
	"github.com/fastcat/wirelink/log"
)

// DumpConfigFlag is the name of the flag to request config dumping
const DumpConfigFlag = "dump"

// VersionFlag is the name of the flag to request printing the program version
const VersionFlag = "version"

// HelpFlag is the name of the flag to request printing program usage
const HelpFlag = "help"

// DebugFlag enables debug logging
const DebugFlag = "debug"

// RouterFlag is the name of the flag to set router mode
const RouterFlag = "router"

// IfaceFlag is the name of the flag to set the wireguard interface to use
const IfaceFlag = "iface"

// ConfigPathFlag is the name of the setting for the config file base path
const ConfigPathFlag = "config-path"

// ChattyFlag is the name of the setting to enable chatty mode
const ChattyFlag = "chatty"

func programName(args []string) string {
	return fmt.Sprintf("%s (%s)", os.Args[0], internal.Version)
}

// Init sets up the config flags and other parsing setup
func Init() (flags *pflag.FlagSet, vcfg *viper.Viper) {
	flags = pflag.NewFlagSet(programName(os.Args), pflag.ContinueOnError)
	vcfg = viper.New()

	flags.Bool(RouterFlag, false, "Is the local device a router (bool, omit for autodetect)")
	vcfg.SetDefault(IfaceFlag, "wg0")
	flags.String("iface", "wg0", "Interface on which to operate")
	vcfg.SetDefault(DumpConfigFlag, false)
	flags.Bool(DumpConfigFlag, false, "Dump configuration instead of running")
	flags.Bool(VersionFlag, false, "Print program version")
	flags.BoolP(HelpFlag, "h", false, "Print program usage")
	vcfg.SetDefault(ConfigPathFlag, "/etc/wireguard")
	// no flag for config-path for now, only env
	vcfg.SetDefault(DebugFlag, false)
	flags.Bool(DebugFlag, false, "Enable debug logging output")

	vcfg.BindPFlags(flags)
	vcfg.SetEnvPrefix("wirelink")
	vcfg.AutomaticEnv()
	// hard to set env vars with hyphens, bash doesn't like it
	vcfg.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	return
}

// Parse reads flags and configs
func Parse(flags *pflag.FlagSet, vcfg *viper.Viper) (ret *ServerData, err error) {
	err = flags.Parse(os.Args[1:])
	if err != nil {
		// TODO: this causes the error to be printed twice: once by flags and once by `main`
		// TODO: this also causes an error to be printed & returned when run with `--help`
		return
	}
	// activate debug logging immediately
	if debug, _ := flags.GetBool(DebugFlag); debug {
		log.SetDebug(true)
	}

	// handle --version and --help specially
	if help, _ := flags.GetBool(HelpFlag); help {
		// if help is requested explicitly, don't send it to stderr
		// have to do this by hand here for now as can't write a Usage
		// func that can see flags.output
		flags.SetOutput(os.Stdout)
		fmt.Fprintf(os.Stdout, "Usage of %s:\n", programName(os.Args))
		flags.PrintDefaults()
		return nil, nil
	}
	if version, _ := flags.GetBool(VersionFlag); version {
		_, err = fmt.Printf("%s\n", programName(os.Args))
		return nil, err
	}

	// setup the config file -- can't do this until after we've parsed the iface flag
	// in theory the config file can override the iface, but ... that would be bad
	// this needs to happen _before_ the `router` processing since the config may set that
	vcfg.SetConfigName(fmt.Sprintf("wirelink.%s", vcfg.GetString(IfaceFlag)))
	// this is perversely recursive
	vcfg.AddConfigPath(vcfg.GetString(ConfigPathFlag))

	err = vcfg.ReadInConfig()
	if err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, harmless
		} else {
			return nil, errors.Wrap(err, "Unable to read config file")
		}
	}

	// load peer configurations
	ret = new(ServerData)
	if err = vcfg.UnmarshalExact(ret); err != nil {
		// TODO: this doesn't print the program name header
		flags.PrintDefaults()
		return nil, errors.Wrapf(err, "Unable to parse config")
	}

	// viper/pflags doesn't have the concept of an optional setting that isn't set
	// have to do some mucking to fake it
	if !vcfg.IsSet(RouterFlag) {
		ret.Router = nil
	}

	return
}
