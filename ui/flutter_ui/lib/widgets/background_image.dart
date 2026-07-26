import 'package:flutter/material.dart';
import '../../theme/colors.dart';

/// Background image widget for the cyberpunk theme.
///
/// Displays the gui-bg.png image with optional opacity for child content.
/// Use this to add the background image to any panel or container.
class BackgroundImage extends StatelessWidget {
  final Widget child;
  final double opacity;

  const BackgroundImage({
    super.key,
    required this.child,
    this.opacity = 1.0,
  });

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        // Background image
        Positioned.fill(
          child: Image.asset(
            'assets/images/gui-bg.png',
            fit: BoxFit.cover,
            opacity: AlwaysStoppedAnimation(opacity),
          ),
        ),
        // Content overlay
        child,
      ],
    );
  }
}

/// Background image with optional color tint overlay.
///
/// For sidebar: use green tint at low opacity
/// For chat area: use no tint (standard background)
class TintedBackgroundImage extends StatelessWidget {
  final Widget child;
  final double imageOpacity;
  final Color? tintColor;
  final double tintOpacity;

  const TintedBackgroundImage({
    super.key,
    required this.child,
    this.imageOpacity = 0.15,
    this.tintColor,
    this.tintOpacity = 0.1,
  });

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        // Background image
        Positioned.fill(
          child: Image.asset(
            'assets/images/gui-bg.png',
            fit: BoxFit.cover,
            opacity: AlwaysStoppedAnimation(imageOpacity),
          ),
        ),
        // Tint overlay (if specified)
        if (tintColor != null)
          Positioned.fill(
            child: Container(
              color: tintColor!.withValues(alpha: tintOpacity),
            ),
          ),
        // Content overlay
        child,
      ],
    );
  }
}

/// Semi-transparent container for chat bubbles with background image.
///
/// Wraps chat content with a 60% opacity background to blend with
/// the main GUI background image.
class ChatBubbleContainer extends StatelessWidget {
  final Widget child;
  final bool isUser;

  const ChatBubbleContainer({
    super.key,
    required this.child,
    required this.isUser,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: (isUser
                ? CyberpunkColors.orangePrimary
                : CyberpunkColors.midGray)
            .withValues(alpha: 0.6),
        border: Border.all(
          color: isUser
              ? CyberpunkColors.orangePrimary
              : CyberpunkColors.lightGray,
          width: 1,
        ),
        borderRadius: BorderRadius.circular(8),
      ),
      child: child,
    );
  }
}
