package server

import (
	"context"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/urfave/cli/v3"
)

var Command = &cli.Command{
	Name:            "server",
	Usage:           "start AsterRouter",
	Action:          config.Manager.Action(action),
	HideHelpCommand: true,
}

func action(ctx context.Context, _ *cli.Command, cfg *config.Config) error {
	return NewApp(&cfg.Server).Run(ctx)
}
