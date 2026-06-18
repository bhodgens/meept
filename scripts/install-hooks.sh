#!/bin/bash
# Install git hooks for Meept
#
# This script installs the pre-commit hooks and makes them executable.
# Run from the project root: ./scripts/install-hooks.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
HOOKS_DIR="$PROJECT_ROOT/.git/hooks"

echo "Installing Meept git hooks..."
echo ""

# Make hooks executable
echo "Making hooks executable..."
chmod +x "$HOOKS_DIR/pre-commit"
chmod +x "$HOOKS_DIR/pre-commit-deferred"
chmod +x "$HOOKS_DIR/pre-commit-feature-docs"

echo "✅ Hooks installed successfully"
echo ""
echo "Installed hooks:"
echo "  - pre-commit (main entry point)"
echo "  - pre-commit-deferred (deferred item validation)"
echo "  - pre-commit-feature-docs (documentation check)"
echo ""
echo "To verify installation:"
echo "  ls -la $HOOKS_DIR/pre-commit*"
echo ""

# Check if aider is available
if command -v aider &> /dev/null; then
  echo "✅ aider found: $(which aider)"
  echo "   The feature-docs hook can use aider to generate documentation."
else
  echo "⚠️  aider not found"
  echo ""
  echo "   The feature-docs hook can use aider to help generate documentation."
  echo "   Install with: pip install aider-chat"
  echo "   Or: brew install aider (if available)"
  echo ""
fi

echo ""
echo "For more information, see: .git/hooks/README.md"
