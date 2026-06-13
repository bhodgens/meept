#!/bin/bash
# Patch flutter_tts plugin for Swift 6 compatibility
# Run this after flutter pub get if warnings appear
# Idempotent - safe to run multiple times

PLUGIN_PATH="$HOME/.pub-cache/hosted/pub.dev/flutter_tts-4.2.5/macos/Classes/FlutterTtsPlugin.swift"

if [ ! -f "$PLUGIN_PATH" ]; then
    echo "Error: flutter_tts plugin not found at $PLUGIN_PATH"
    exit 1
fi

# Check if already patched
if grep -q "@unknown default" "$PLUGIN_PATH"; then
    echo "flutter_tts already patched for Swift 6 compatibility. Skipping."
    exit 0
fi

echo "Patching flutter_tts for Swift 6 compatibility..."

# Use sed to add @unknown default after the last case in each switch
# AVSpeechSynthesisVoiceQuality - after "case .enhanced:"
sed -i '' '/case \.enhanced:/,/}/ {
    /return "enhanced"/a\
        @unknown default:\
            return "unknown"
}' "$PLUGIN_PATH"

# AVSpeechSynthesisVoiceGender - after "case .unspecified:"
sed -i '' '/case \.unspecified:/,/}/ {
    /return "unspecified"/a\
        @unknown default:\
            return "unknown"
}' "$PLUGIN_PATH"

echo "Swift 6 patch applied successfully!"
echo "Backup saved at: $PLUGIN_PATH.backup"
