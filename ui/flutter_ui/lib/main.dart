import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'theme/cyberpunk_theme.dart';
import 'features/home/home_screen.dart';

void main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Initialize Hive storage
  // await Hive.initFlutter();
  // await Hive.openBox('settings');
  // await Hive.openBox('cache');

  runApp(
    const ProviderScope(
      child: CyberpunkApp(),
    ),
  );
}

class CyberpunkApp extends StatelessWidget {
  const CyberpunkApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Meept Cyberpunk UI',
      debugShowCheckedModeBanner: false,
      theme: CyberpunkTheme.darkTheme,
      home: const HomeScreen(),
    );
  }
}
