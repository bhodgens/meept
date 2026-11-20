package bot

// Wiring documents how the bot framework integrates into the daemon.
//
// IMPLEMENTED in internal/daemon/components.go (NewComponents):
//   - BotStore and BotManager fields on Components struct
//   - Bot framework initialization when cfg.Bots.Enabled is true
//   - BotManager.StartAll() called in Components.Start()
//   - BotManager.StopAll() and BotStore.Close() called in Components.Stop()
//
// IMPLEMENTED in internal/daemon/daemon.go:
//   - Bot RPC handlers registered with rpcServer via botpkg.NewRPCHandler
//
// IMPLEMENTED in cmd/meept/bot_cmd.go:
//   - bots subcommand with list, show, create, delete, pause, resume

// WiringPlaceholder ensures this file is part of the package.
const WiringPlaceholder = "bot framework is wired into daemon"
