package main

import (
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/configui"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [section]",
		Short: "configure meept settings",
		Long:  "Interactive configuration editor for all meept config files.\nRun without arguments to open the TUI menu.",
		RunE:  runConfigTUI,
	}

	cmd.AddCommand(newConfigListCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newOAuthCmd())
	cmd.AddCommand(newConfigSyncCmd())

	return cmd
}

func runConfigTUI(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return configui.RunWithSection(args[0])
	}
	return configui.RunApp()
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list config file paths and status",
		RunE: func(cmd *cobra.Command, args []string) error {
			files := []struct {
				Name string
				Path string
			}{
				{"meept.json5", configui.ConfigFilePath("meept.json5")},
				{"models.json5", configui.ConfigFilePath("models.json5")},
				{"mcp_servers.json5", configui.ConfigFilePath("mcp_servers.json5")},
				{"client.json5", configui.ConfigFilePath("client.json5")},
				{"presets.json5", configui.ConfigFilePath("presets.json5")},
			}
			for _, f := range files {
				status := "missing"
				if _, err := os.Stat(f.Path); err == nil {
					status = "exists"
				}
				fmt.Printf("%-20s %s  (%s)\n", f.Name, f.Path, status)
			}
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <keypath>",
		Short: "get a config value by dot-notation path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			val, err := configui.GetKeypath(cfg, args[0])
			if err != nil {
				return err
			}
			fmt.Println(val)
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <keypath> <value>",
		Short: "set a config value by dot-notation path",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			if err := configui.SetKeypath(cfg, args[0], args[1]); err != nil {
				return err
			}
			return configui.SaveMainConfig(cfg)
		},
	}
}
