package bot

// Wiring documents how to integrate the bot framework into the daemon.
//
// In internal/daemon/components.go, after the scheduler is set up:
//
//	if cfg.Bots.Enabled {
//	    botStore, err := bot.NewStore(filepath.Join(cfg.Bots.DataDir, "bots.db"))
//	    if err != nil {
//	        return fmt.Errorf("bot store: %w", err)
//	    }
//	    botRouter := bot.NewEventActionRouter(msgBus, nil) // handler wired separately
//	    botManager := bot.NewManager(botStore, botRouter)
//	    botRPCHandler := bot.NewRPCHandler(botManager)
//
//	    // Register RPC handlers
//	    for method, handler := range botRPCHandler.Handlers() {
//	        rpcServer.RegisterHandler(method, handler)
//	    }
//
//	    // Register webhook endpoint
//	    if cfg.Bots.WebhookEnabled {
//	        webhookHandler := bot.NewWebhookHandler(botManager)
//	        httpMux.Handle("/api/v1/bot/", webhookHandler)
//	    }
//
//	    // Start all enabled bots
//	    if err := botManager.StartAll(ctx); err != nil {
//	        logger.Error("failed to start bots", "error", err)
//	    }
//
//	    c.BotManager = botManager
//	    c.BotStore = botStore
//	}
//
// In internal/daemon/daemon.go shutdown:
//
//	if c.BotManager != nil {
//	    c.BotManager.StopAll()
//	}
//	if c.BotStore != nil {
//	    c.BotStore.Close()
//	}
//
// In cmd/meept/main.go, register the bots subcommand:
//
//	rootCmd.AddCommand(botsCmd)

// WiringPlaceholder ensures this file is part of the package.
const WiringPlaceholder = "see comments for daemon integration instructions"
