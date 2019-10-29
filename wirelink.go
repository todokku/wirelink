package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/fastcat/wirelink/config"
	"github.com/fastcat/wirelink/log"
	"github.com/fastcat/wirelink/server"
	"github.com/pkg/errors"
)

func main() {
	err := realMain()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		defer os.Exit(1)
	}
}

func realMain() error {
	wgc, err := wgctrl.New()
	if err != nil {
		return errors.Wrapf(err, "Unable to initialize wgctrl")
	}

	flags, vcfg := config.Init()
	var configData *config.ServerData
	if configData, err = config.Parse(flags, vcfg); err != nil {
		return errors.Wrapf(err, "Failed to load config")
	}

	var serverConfig *config.Server
	if serverConfig, err = configData.Parse(vcfg, wgc); err != nil {
		// TODO: this doesn't print the program name header
		flags.PrintDefaults()
		return errors.Wrapf(err, "Unable to load configuration")
	}
	if serverConfig == nil {
		// config dump was requested
		return nil
	}

	server, err := server.Create(wgc, serverConfig)
	if err != nil {
		return errors.Wrapf(err, "Unable to initialize server for interface %s", serverConfig.Iface)
	}
	defer server.Close()

	nodeTypeDesc := "leaf"
	if serverConfig.IsRouter {
		nodeTypeDesc = "router"
	}
	log.Info("Server running on {%s} [%v]:%v (%s)", serverConfig.Iface, server.Address(), server.Port(), nodeTypeDesc)

	sigs := make(chan os.Signal, 5)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	onStopped := server.OnStopped()

DONE:
	for {
		select {
		case sig := <-sigs:
			if sig == syscall.SIGUSR1 {
				server.RequestPrint()
			} else {
				log.Info("Received signal %v, stopping", sig)
				// request stop in the background, we'll catch the channel message when it's complete
				go server.Stop()
			}
		case exitOk := <-onStopped:
			if !exitOk {
				log.Error("Server hit an error")
				defer os.Exit(1)
			} else {
				log.Info("Server stopped")
				server.RequestPrint()
			}
			break DONE
		}
	}

	// server.Close is handled by defer above
	return nil
}
