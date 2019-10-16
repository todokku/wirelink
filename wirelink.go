package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/fastcat/wirelink/server"
	"github.com/fastcat/wirelink/trust"
	"github.com/pkg/errors"
)

func main() {
	wgc, err := wgctrl.New()
	if err != nil {
		panic(err)
	}

	const RouterFlag = "router"
	isRouter := flag.Bool(RouterFlag, false, "Is the local device a router (default is autodetected)")
	iface := "wg0"
	// TODO: make the env var formulation & parsing from flags more programatic
	// pull this one from the env before declaring the flag so the env var can be the flag "default"
	if ifaceEnv, ifaceEnvPresent := os.LookupEnv("WIRELINK_IFACE"); ifaceEnvPresent {
		iface = ifaceEnv
	}
	flag.StringVar(&iface, "iface", iface, "Interface on which to operate")
	isRouterSet := false
	// TODO: make the env var formulation & parsing from flags more programatic
	if routerEnv, routerEnvPresent := os.LookupEnv("WIRELINK_ROUTER"); routerEnvPresent {
		if routerEnvValue, err := strconv.ParseBool(routerEnv); err == nil {
			*isRouter = routerEnvValue
			isRouterSet = true
		} else {
			panic(errors.Wrapf(err, "Bad value for WIRELINK_ROUTER"))
		}
	}
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == RouterFlag {
			isRouterSet = true
		}
	})
	if !isRouterSet {
		// try to auto-detect router mode
		// if there are no other routers ... then we're probably a router
		// this is pretty weak, better would be to check if our IP is within some other peer's AllowedIPs
		dev, err := wgc.Device(iface)
		if err != nil {
			panic(err)
		}

		otherRouters := false
		for _, p := range dev.Peers {
			if trust.IsRouter(&p) {
				otherRouters = true
				break
			}
		}

		if !otherRouters {
			*isRouter = true
		}
	}

	server, err := server.Create(wgc, iface, 0, *isRouter)
	if err != nil {
		panic(err)
	}
	defer server.Close()

	routerString := "leaf"
	if *isRouter {
		routerString = "router"
	}
	fmt.Printf("Server running on {%s} [%v]:%v (%s)\n", iface, server.Address(), server.Port(), routerString)

	sigs := make(chan os.Signal, 5)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

DONE:
	for {
		select {
		case sig := <-sigs:
			if sig == syscall.SIGUSR1 {
				server.RequestPrint()
			} else {
				fmt.Printf("Received signal %v, stopping\n", sig)
				break DONE
			}
		}
	}

	fmt.Println("Stopping server")
	server.RequestPrint()
	server.Stop()
}
