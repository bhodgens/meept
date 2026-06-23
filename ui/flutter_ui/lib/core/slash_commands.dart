/// Built-in slash commands and command registry for the chat input.
library;

/// A slash command definition.
class SlashCommand {
  final String name;
  final String description;
  final String? usage;
  final Future<String?> Function(String args)? handler;

  const SlashCommand({
    required this.name,
    required this.description,
    this.usage,
    this.handler,
  });
}

/// Registry of all available slash commands.
class SlashCommandRegistry {
  final List<SlashCommand> _builtIn = [];
  final List<SlashCommand> _custom = [];

  SlashCommandRegistry() {
    _builtIn.addAll(_defaultCommands);
  }

  /// All available commands (built-in + custom).
  List<SlashCommand> get all => [..._builtIn, ..._custom];

  /// Find commands matching a prefix (e.g., "/h" matches "/help").
  List<SlashCommand> match(String prefix) {
    final normalized = prefix.startsWith('/') ? prefix : '/$prefix';
    return all.where((cmd) => cmd.name.startsWith(normalized)).toList();
  }

  /// Get a single command by exact name.
  SlashCommand? get(String name) {
    final n = name.startsWith('/') ? name : '/$name';
    final matches = all.where((cmd) => cmd.name == n);
    return matches.isEmpty ? null : matches.first;
  }

  /// Add custom commands (fetched from daemon API).
  void setCustomCommands(List<SlashCommand> commands) {
    _custom
      ..clear()
      ..addAll(commands);
  }
}

/// Default built-in slash commands.
const _defaultCommands = <SlashCommand>[
  SlashCommand(name: '/help', description: 'show available commands'),
  SlashCommand(name: '/new', description: 'create a new session'),
  SlashCommand(name: '/clear', description: 'clear chat history'),
  SlashCommand(name: '/stop', description: 'stop the active agent'),
  SlashCommand(name: '/status', description: 'show daemon status'),
  SlashCommand(name: '/session', description: 'show current session info', usage: '/session [id]'),
  SlashCommand(name: '/model', description: 'switch or show model', usage: '/model [name]'),
  SlashCommand(name: '/compact', description: 'compact conversation context'),
  SlashCommand(name: '/retry', description: 'retry last message'),
  SlashCommand(name: '/undo', description: 'undo last exchange'),
  SlashCommand(name: '/usage', description: 'show token usage stats'),
  SlashCommand(name: '/vim', description: 'toggle vim mode'),
  SlashCommand(name: '/task', description: 'create a new task', usage: '/task <title>'),
  SlashCommand(name: '/cancel', description: 'cancel the running task'),
  SlashCommand(name: '/amend', description: 'amend last message', usage: '/amend <text>'),
  SlashCommand(name: '/interrupt', description: 'interrupt active agent'),
  SlashCommand(name: '/tasks', description: 'list tasks'),
  SlashCommand(name: '/diff', description: 'show git diff'),
  SlashCommand(name: '/edit', description: 'edit a file', usage: '/edit <path>'),
  SlashCommand(name: '/plan', description: 'manage plans', usage: '/plan [list|show|approve]'),
  SlashCommand(name: '/review', description: 'start code review'),
  SlashCommand(name: '/project', description: 'manage projects', usage: '/project [list|add]'),
  SlashCommand(
      name: '/skill',
      description: 'list, search, or show skill details',
      usage: '/skill [name|search <query>]'),
];
