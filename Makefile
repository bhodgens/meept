.PHONY: help install install-dev setup start stop restart cli menubar test lint format clean install-service uninstall

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Setup:"
	@echo "  install          Create venv and install meept"
	@echo "  install-dev      Create venv and install with dev dependencies"
	@echo "  setup            Create ~/.meept directory and default config"
	@echo ""
	@echo "Daemon:"
	@echo "  start            Start the daemon (background)"
	@echo "  stop             Stop the running daemon"
	@echo "  restart          Stop and restart the daemon"
	@echo ""
	@echo "Interfaces:"
	@echo "  cli              Launch the interactive CLI"
	@echo "  menubar          Build the Tauri menubar app"
	@echo ""
	@echo "Development:"
	@echo "  test             Run the test suite"
	@echo "  lint             Check code style with ruff"
	@echo "  format           Auto-format code with ruff"
	@echo "  clean            Remove venv, build artifacts, and __pycache__"
	@echo ""
	@echo "Service:"
	@echo "  install-service  Install as a system service (launchd/systemd)"
	@echo "  uninstall        Remove the system service"

VENV := .venv
PYTHON := $(VENV)/bin/python
PIP := $(VENV)/bin/pip
MEEPT_HOME := $(HOME)/.meept
SOCK := $(MEEPT_HOME)/meept.sock
PID := $(MEEPT_HOME)/meept.pid

install:
	python3 -m venv $(VENV)
	$(PIP) install -e ".[all]"

install-dev:
	python3 -m venv $(VENV)
	$(PIP) install -e ".[all,dev]"

setup:
	mkdir -p $(MEEPT_HOME)
	@if [ ! -f $(MEEPT_HOME)/meept.toml ]; then \
		cp config/meept.toml $(MEEPT_HOME)/meept.toml; \
		echo "Created $(MEEPT_HOME)/meept.toml - edit with your LLM settings"; \
	fi
	@if [ ! -d $(MEEPT_HOME)/plugins ]; then \
		mkdir -p $(MEEPT_HOME)/plugins; \
	fi
	@if [ ! -d $(MEEPT_HOME)/memory ]; then \
		mkdir -p $(MEEPT_HOME)/memory; \
	fi

start: setup
	$(PYTHON) -m meept

stop:
	@if [ -f $(PID) ]; then \
		kill $$(cat $(PID)) 2>/dev/null || true; \
		rm -f $(PID) $(SOCK); \
		echo "Daemon stopped"; \
	else \
		echo "No PID file found"; \
	fi

restart: stop start

cli:
	$(PYTHON) -m cli

menubar:
	cd menubar && npm install && cargo tauri build

test:
	$(PYTHON) -m pytest tests/ -v --tb=short

lint:
	$(VENV)/bin/ruff check src/ cli/ tests/
	$(VENV)/bin/ruff format --check src/ cli/ tests/

format:
	$(VENV)/bin/ruff check --fix src/ cli/ tests/
	$(VENV)/bin/ruff format src/ cli/ tests/

clean:
	rm -rf $(VENV) dist/ build/ *.egg-info
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true

install-service:
	@case "$$(uname)" in \
		Darwin) \
			sed "s|{{PYTHON}}|$$(pwd)/$(PYTHON)|g; s|{{MEEPT_DIR}}|$$(pwd)|g" \
				service/com.meept.daemon.plist > ~/Library/LaunchAgents/com.meept.daemon.plist; \
			launchctl load ~/Library/LaunchAgents/com.meept.daemon.plist; \
			echo "Installed launchd service"; \
			;; \
		Linux) \
			sed "s|{{PYTHON}}|$$(pwd)/$(PYTHON)|g; s|{{MEEPT_DIR}}|$$(pwd)|g" \
				service/meept.service > ~/.config/systemd/user/meept.service; \
			systemctl --user daemon-reload; \
			systemctl --user enable meept; \
			systemctl --user start meept; \
			echo "Installed systemd service"; \
			;; \
	esac

uninstall:
	@case "$$(uname)" in \
		Darwin) \
			launchctl unload ~/Library/LaunchAgents/com.meept.daemon.plist 2>/dev/null || true; \
			rm -f ~/Library/LaunchAgents/com.meept.daemon.plist; \
			;; \
		Linux) \
			systemctl --user stop meept 2>/dev/null || true; \
			systemctl --user disable meept 2>/dev/null || true; \
			rm -f ~/.config/systemd/user/meept.service; \
			;; \
	esac
	@echo "Service uninstalled (data preserved at $(MEEPT_HOME))"
