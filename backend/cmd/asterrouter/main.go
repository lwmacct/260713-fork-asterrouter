package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	appserver "github.com/astercloud/asterrouter/backend/internal/appcmd/server"
	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/urfave/cli/v3"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		printVersion()
		return
	}
	versionCommand := &cli.Command{
		Name:   "version",
		Usage:  "print build version information",
		Action: func(context.Context, *cli.Command) error { printVersion(); return nil },
	}
	command := &cli.Command{
		Name:            "asterrouter",
		Usage:           "AI gateway control plane",
		Commands:        []*cli.Command{appserver.Command, versionCommand},
		HideHelpCommand: true,
		Action: func(_ context.Context, command *cli.Command) error {
			return cli.ShowSubcommandHelp(command)
		},
	}
	config.Manager.MustConfigure(command)
	if err := command.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func printVersion() {
	fmt.Printf("asterrouter %s\ncommit: %s\nbuilt: %s\nbuild_type: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date, buildinfo.BuildType)
}
