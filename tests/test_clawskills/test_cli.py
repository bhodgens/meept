"""Tests for the clawskills CLI subcommand handlers."""

from __future__ import annotations

import argparse
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from meept.clawskills.cli import _build_parser, handle_clawskills


class TestParser:
    def test_search_command(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["search", "code review"])
        assert args.command == "search"
        assert args.query == "code review"

    def test_install_command(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["install", "gifgrep", "--version", "1.0.0"])
        assert args.command == "install"
        assert args.slug == "gifgrep"
        assert args.version == "1.0.0"

    def test_install_default_version(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["install", "gifgrep"])
        assert args.version is None

    def test_update_single(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["update", "gifgrep"])
        assert args.command == "update"
        assert args.slug == "gifgrep"
        assert args.all is False

    def test_update_all(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["update", "--all"])
        assert args.command == "update"
        assert args.all is True

    def test_list_command(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["list"])
        assert args.command == "list"

    def test_inspect_command(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["inspect", "gifgrep"])
        assert args.command == "inspect"
        assert args.slug == "gifgrep"

    def test_info_command(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["info", "gifgrep"])
        assert args.command == "info"
        assert args.slug == "gifgrep"

    def test_remove_command(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["remove", "gifgrep"])
        assert args.command == "remove"
        assert args.slug == "gifgrep"

    def test_registry_flag(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["--registry", "https://custom.ai", "search", "test"])
        assert args.registry == "https://custom.ai"

    def test_search_limit(self) -> None:
        parser = _build_parser()
        args = parser.parse_args(["search", "test", "--limit", "5"])
        assert args.limit == 5

    def test_no_command_prints_help(self) -> None:
        with pytest.raises(SystemExit):
            handle_clawskills([])
