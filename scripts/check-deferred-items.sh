#!/bin/bash
# check-deferred-items.sh - Scan findings documents for unresolved deferred items
#
# Usage:
#   ./scripts/check-deferred-items.sh              # Check all findings docs
#   ./scripts/check-deferred-items.sh --staged     # Check only staged changes
#   ./scripts/check-deferred-items.sh --recent     # Check recent session docs
#

set -e

# Colors
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
FINDINGS_DIR="docs/plans"
OUTPUT_FILE="/tmp/deferred-items-report.txt"

# Function to check if a file has a corresponding deferred implementation plan
has_deferred_plan() {
  local file="$1"
  local basename=$(basename "$file" .md)
  local plan="${FINDINGS_DIR}/${basename}-deferred-implementation.md"
  
  # Check if plan exists
  if [ -f "$plan" ]; then
    # Check if plan says all items are resolved
    if grep -q "100%\|all resolved\|Remaining: 0" "$plan" 2>/dev/null; then
      return 0  # Plan exists and all resolved
    fi
    return 1  # Plan exists but not all resolved
  fi
  return 1  # No plan
}

# Function to check a single file for ACTUAL unresolved deferred items
check_file() {
  local file="$1"
  local unresolved_lines=""
  
  # Skip if has a "all resolved" deferred plan
  if has_deferred_plan "$file"; then
    return 1
  fi
  
  # Look for deferred items that aren't resolution keywords
  # More aggressive filtering to avoid historical mentions
  unresolved_lines=$(grep -in "deferred" "$file" 2>/dev/null | \
    grep -iv "resolved\|fixed\|closed\|completion rate: 100%\|all resolved\|remaining: 0" | \
    grep -iv "## Issues Deferred\|### Deferred\|^|.*DEFERRED\|deferred item\|deferred findings\|deferred —" | \
    grep -iv "were deferred\|initially deferred\|previously deferred" | \
    grep -iv "recommend.*PR\|categorized by.*PR\|follow-up PR" | \
    grep -iv "structural gap\|architectural decision\|intentional" || true)
  
  if [ -n "$unresolved_lines" ]; then
    echo "$unresolved_lines"
  fi
}

# Function to generate report header
report_header() {
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "       DEFERRED ITEMS REPORT"
  echo "       Generated: $(date '+%Y-%m-%d %H:%M:%S')"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
  echo "NOTE: This report excludes findings with 'all resolved' deferred plans"
  echo "      and historical/section headers mentioning 'deferred'"
  echo ""
}

# Main check function
run_check() {
  local files_to_check=()
  local total_deferred=0
  local files_with_deferred=0
  
  # Determine which files to check
  case "${1:-all}" in
    --staged)
      echo -e "${BLUE}📦 Checking staged changes...${NC}"
      while IFS= read -r file; do
        [ -n "$file" ] && files_to_check+=("$file")
      done < <(git diff --cached --name-only 2>/dev/null | grep "docs/plans/.*findings" || true)
      ;;
    --recent)
      echo -e "${BLUE}📅 Checking recent session documents (last 7 days)...${NC}"
      while IFS= read -r file; do
        [ -n "$file" ] && files_to_check+=("$file")
      done < <(find "$FINDINGS_DIR" -name "*.md" -mtime -7 2>/dev/null | grep -i findings || true)
      ;;
    *)
      echo -e "${BLUE}🔍 Checking all findings documents...${NC}"
      while IFS= read -r file; do
        # Skip deferred implementation plans themselves
        [ -n "$file" ] && [[ "$file" != *"deferred-implementation"* ]] && files_to_check+=("$file")
      done < <(find "$FINDINGS_DIR" -name "*findings*.md" 2>/dev/null || true)
      ;;
  esac
  
  if [ ${#files_to_check[@]} -eq 0 ]; then
    echo -e "${GREEN}✅ No findings documents to check${NC}"
    return 0
  fi
  
  echo ""
  report_header > "$OUTPUT_FILE"
  
  # Check each file
  for file in "${files_to_check[@]}"; do
    if [ -f "$file" ]; then
      result=$(check_file "$file")
      if [ -n "$result" ]; then
        files_with_deferred=$((files_with_deferred + 1))
        count=$(echo "$result" | wc -l | tr -d ' ')
        total_deferred=$((total_deferred + count))
        
        echo -e "\n${YELLOW}📄 $file${NC} ($count items)" | tee -a "$OUTPUT_FILE"
        echo "$result" | while read -r line; do
          echo "   $line" | tee -a "$OUTPUT_FILE"
        done
        echo ""
      fi
    fi
  done
  
  # Summary
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" | tee -a "$OUTPUT_FILE"
  echo ""
  if [ $files_with_deferred -eq 0 ]; then
    echo -e "${GREEN}✅ No unresolved deferred items found${NC}" | tee -a "$OUTPUT_FILE"
    echo ""
    echo "All deferred findings from reviewed rounds have been resolved!" | tee -a "$OUTPUT_FILE"
  else
    echo -e "${RED}📊 SUMMARY:${NC}" | tee -a "$OUTPUT_FILE"
    echo "   Files with deferred items: $files_with_deferred" | tee -a "$OUTPUT_FILE"
    echo "   Total deferred items: $total_deferred" | tee -a "$OUTPUT_FILE"
    echo ""
    echo -e "${YELLOW}📋 RECOMMENDED ACTIONS:${NC}" | tee -a "$OUTPUT_FILE"
    echo "   1. Review each deferred item above" | tee -a "$OUTPUT_FILE"
    echo "   2. Create docs/plans/[review]-deferred-implementation.md" | tee -a "$OUTPUT_FILE"
    echo "   3. Resolve items or document as intentional" | tee -a "$OUTPUT_FILE"
    echo ""
    echo "📖 See CLAUDE.md 'Deferred Item Resolution Protocol' for details" | tee -a "$OUTPUT_FILE"
  fi
  
  echo ""
  echo "📄 Full report saved to: $OUTPUT_FILE"
  
  # Return non-zero if deferred items found
  [ $files_with_deferred -eq 0 ]
}

# Help message
show_help() {
  cat << HELP
Usage: $0 [OPTIONS]

Check findings documents for unresolved deferred items.
This script intelligently filters out:
  - Historical mentions of "deferred" (section headers, summaries)
  - Items covered by "all resolved" deferred implementation plans
  - Items documented as intentional architectural decisions

Options:
  --staged     Check only staged git changes
  --recent     Check documents modified in last 7 days
  --help, -h   Show this help message

Examples:
  $0                    # Check all findings documents
  $0 --staged           # Check staged changes before commit
  $0 --recent           # Check recent session documents

Output:
  Returns exit code 0 if no deferred items found
  Returns exit code 1 if deferred items found (for CI/CD integration)

HELP
}

# Parse arguments
case "${1:-}" in
  --help|-h)
    show_help
    exit 0
    ;;
  --staged|--recent|all)
    run_check "$1"
    exit $?
    ;;
  "")
    run_check "all"
    exit $?
    ;;
  *)
    echo "Unknown option: $1"
    show_help
    exit 1
    ;;
esac
