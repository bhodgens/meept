# Meept Flutter Client Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update the Meept Flutter client with a new 4-tab navigation structure (Chat, Sessions, Tasks, Agents) featuring the ORANGE VOID cyberpunk theme with orange/black color scheme, Source Code Pro font, and status-based UI patterns.

**Architecture:** The app uses a top tab bar navigation with 4 tabs. Chat tab is a 3-pane layout (transcript, main view, sidebar). Sessions tab shows session list with detail pane. Tasks tab shows tasks with agent status. Agents tab shows agents grouped by task. All UI text is lowercase per project conventions.

**Tech Stack:** Flutter 3.x, Riverpod 2.x for state management, Dio 5.x for HTTP, web_socket_channel for real-time updates, google_fonts for Source Code Pro.

---

## File Structure

**Existing files to modify:**
- `ui/flutter_ui/lib/main.dart` - App entry point, theme
- `ui/flutter_ui/lib/features/home/home_screen.dart` - Main screen structure
- `ui/flutter_ui/lib/features/home/tab_content.dart` - Tab content routing
- `ui/flutter_ui/lib/theme/colors.dart` - Color palette update
- `ui/flutter_ui/lib/theme/typography.dart` - Font configuration

**Files to create:**
- `ui/flutter_ui/lib/features/chat/chat_tab.dart` - New unified chat tab view
- `ui/flutter_ui/lib/features/sessions/sessions_list.dart` - Sessions list widget
- `ui/flutter_ui/lib/features/sessions/sessions_detail.dart` - Session detail pane
- `ui/flutter_ui/lib/features/tasks/tasks_list.dart` - Tasks list widget
- `ui/flutter_ui/lib/features/tasks/tasks_detail.dart` - Task detail pane with agents
- `ui/flutter_ui/lib/features/agents/agents_list.dart` - Agents grouped by task
- `ui/flutter_ui/lib/widgets/tab_bar.dart` - Custom top tab bar widget
- `ui/flutter_ui/lib/models/session.dart` - Session data model
- `ui/flutter_ui/lib/models/task.dart` - Task data model
- `ui/flutter_ui/lib/models/agent.dart` - Agent data model

---

## Sprint 1: Theme and Core Infrastructure

### Task 1: Update Color Scheme to ORANGE VOID Theme

**Files:**
- Modify: `ui/flutter_ui/lib/theme/colors.dart`

- [ ] **Step 1: Update colors.dart with ORANGE VOID palette**

Replace the existing colors with:

```dart
import 'package:flutter/material.dart';

/// ORANGE VOID cyberpunk color palette
abstract class CyberpunkColors {
  // Base colors
  static const Color black = Color(0xFF000000);
  static const Color darkGray = Color(0xFF1A1A1A);
  static const Color midGray = Color(0xFF2A2A2A);
  static const Color lightGray = Color(0xFF333333);

  // Primary - Orange spectrum
  static const Color orangePrimary = Color(0xFFFF6600);
  static const Color orangeBright = Color(0xFFFF8800);
  static const Color orangeDark = Color(0xFFCC5500);
  static const Color orangeGlow = Color(0xFFFFAA33);
  static const Color orangeAccent = Color(0xFFFF9933);

  // Secondary accents
  static const Color cyanAccent = Color(0xFF00FFFF);
  static const Color greenSuccess = Color(0xFF00FFAA);
  static const Color redAlert = Color(0xFFFF3366);
  static const Color yellowWarning = Color(0xFFFFCC00);

  // Terminal colors
  static const Color terminalGreen = Color(0xFF33FF33);
  static const Color terminalAmber = Color(0xFFFFB000);

  // Transparent variants
  static Color orangeTransparent(double opacity) =>
      orangePrimary.withOpacity(opacity);
  static Color blackTransparent(double opacity) =>
      black.withOpacity(opacity);
}
```

- [ ] **Step 2: Commit the color changes**

```bash
cd ui/flutter_ui
git add lib/theme/colors.dart
git commit -m "feat(ui): update to ORANGE VOID color scheme"
```

---

### Task 2: Update Typography to Source Code Pro

**Files:**
- Modify: `ui/flutter_ui/lib/theme/typography.dart`
- Modify: `ui/flutter_ui/pubspec.yaml`

- [ ] **Step 1: Add Source Code Pro to pubspec.yaml fonts**

Add the font reference:
```yaml
flutter:
  fonts:
    - family: SourceCodePro
      fonts:
        - asset: assets/fonts/SourceCodePro-Regular.ttf
        - asset: assets/fonts/SourceCodePro-Bold.ttf
          weight: 700
```

- [ ] **Step 2: Download Source Code Pro fonts**

```bash
cd ui/flutter_ui
mkdir -p assets/fonts
curl -L -o assets/fonts/SourceCodePro-Regular.ttf https://github.com/adobe-fonts/source-code-pro/raw/release/OTF/SourceCodePro-Regular.otf
curl -L -o assets/fonts/SourceCodePro-Bold.ttf https://github.com/adobe-fonts/source-code-pro/raw/release/OTF/SourceCodePro-Bold.otf
```

- [ ] **Step 3: Update typography.dart to use Source Code Pro**

```dart
import 'package:flutter/material.dart';

/// ORANGE VOID typography configuration
abstract class CyberpunkTypography {
  static const String primaryFont = 'SourceCodePro';
  static const String displayFont = 'SourceCodePro';

  // Text styles - all lowercase per project convention
  static const TextStyle displayLarge = TextStyle(
    fontFamily: primaryFont,
    fontSize: 32,
    fontWeight: FontWeight.bold,
    letterSpacing: 3,
  );

  static const TextStyle displayMedium = TextStyle(
    fontFamily: primaryFont,
    fontSize: 24,
    fontWeight: FontWeight.bold,
    letterSpacing: 2,
  );

  static const TextStyle headlineLarge = TextStyle(
    fontFamily: primaryFont,
    fontSize: 20,
    fontWeight: FontWeight.bold,
    letterSpacing: 2,
  );

  static const TextStyle headlineMedium = TextStyle(
    fontFamily: primaryFont,
    fontSize: 18,
    fontWeight: FontWeight.w600,
    letterSpacing: 1,
  );

  static const TextStyle headlineSmall = TextStyle(
    fontFamily: primaryFont,
    fontSize: 16,
    fontWeight: FontWeight.w600,
  );

  static const TextStyle bodyLarge = TextStyle(
    fontFamily: primaryFont,
    fontSize: 14,
    letterSpacing: 0.5,
  );

  static const TextStyle bodyMedium = TextStyle(
    fontFamily: primaryFont,
    fontSize: 13,
  );

  static const TextStyle bodySmall = TextStyle(
    fontFamily: primaryFont,
    fontSize: 11,
    color: Colors.grey,
  );

  static const TextStyle label = TextStyle(
    fontFamily: primaryFont,
    fontSize: 12,
    fontWeight: FontWeight.w600,
    letterSpacing: 1,
  );

  static const TextStyle button = TextStyle(
    fontFamily: primaryFont,
    fontSize: 12,
    fontWeight: FontWeight.bold,
    letterSpacing: 1,
  );

  static const TextStyle code = TextStyle(
    fontFamily: 'monospace',
    fontSize: 12,
  );
}
```

- [ ] **Step 4: Commit typography changes**

```bash
cd ui/flutter_ui
git add pubspec.yaml lib/theme/typography.dart assets/fonts/
git commit -m "feat(ui): add Source Code Pro font for ORANGE VOID theme"
```

---

### Task 3: Create Custom Top Tab Bar Widget

**Files:**
- Create: `ui/flutter_ui/lib/widgets/tab_bar.dart`

- [ ] **Step 1: Create the custom tab bar widget**

```dart
import 'package:flutter/material.dart';
import '../theme/colors.dart';
import '../theme/typography.dart';

class OrangeVoidTabBar extends StatelessWidget {
  final List<String> tabs;
  final int selectedIndex;
  final ValueChanged<int> onTabSelected;

  const OrangeVoidTabBar({
    super.key,
    required this.tabs,
    required this.selectedIndex,
    required this.onTabSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        color: CyberpunkColors.black,
        border: Border(
          bottom: BorderSide(
            color: CyberpunkColors.orangeDark,
            width: 2,
          ),
        ),
      ),
      child: Row(
        children: List.generate(tabs.length, (index) {
          final isSelected = selectedIndex == index;
          return Expanded(
            child: InkWell(
              onTap: () => onTabSelected(index),
              child: Container(
                padding: const EdgeInsets.symmetric(vertical: 16),
                decoration: BoxDecoration(
                  border: Border(
                    bottom: BorderSide(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : Colors.transparent,
                      width: 3,
                    ),
                  ),
                ),
                child: Center(
                  child: Text(
                    tabs[index].toLowerCase(),
                    style: CyberpunkTypography.label.copyWith(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : Colors.grey,
                      letterSpacing: 2,
                    ),
                  ),
                ),
              ),
            ),
          );
        }),
      ),
    );
  }
}
```

- [ ] **Step 2: Commit the tab bar widget**

```bash
cd ui/flutter_ui
git add lib/widgets/
git commit -m "feat(ui): add custom ORANGE VOID tab bar widget"
```

---

## Sprint 2: Chat Tab Implementation

### Task 4: Create Chat Tab with 3-Pane Layout

**Files:**
- Create: `ui/flutter_ui/lib/features/chat/chat_tab.dart`
- Modify: `ui/flutter_ui/lib/features/chat/chat_view.dart`
- Modify: `ui/flutter_ui/lib/features/sidebar/tools_panel.dart`

- [ ] **Step 1: Create chat_tab.dart with 3-pane layout**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'chat_view.dart';
import '../sidebar/tools_panel.dart';

class ChatTab extends StatefulWidget {
  final String sessionId;

  const ChatTab({super.key, required this.sessionId});

  @override
  State<ChatTab> createState() => _ChatTabState();
}

class _ChatTabState extends State<ChatTab> {
  bool _isSidebarCollapsed = false;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          // Left pane: Chat transcript
          Expanded(
            flex: 3,
            child: Container(
              decoration: BoxDecoration(
                border: Border(
                  right: BorderSide(
                    color: CyberpunkColors.orangeDark.withOpacity(0.3),
                    width: 1,
                  ),
                ),
              ),
              child: ChatView(sessionId: widget.sessionId),
            ),
          ),
          // Right pane: Tools sidebar
          if (!_isSidebarCollapsed)
            ToolsPanel(
              isExpanded: !_isSidebarCollapsed,
              onCollapseToggle: () =>
                  setState(() => _isSidebarCollapsed = !_isSidebarCollapsed),
            ),
        ],
      ),
    );
  }
}
```

- [ ] **Step 2: Update chat_view.dart to include header animation**

Add the animated header similar to the existing meept client sidebar with the collapse/expand animation.

- [ ] **Step 3: Commit chat tab implementation**

```bash
cd ui/flutter_ui
git add lib/features/chat/chat_tab.dart
git commit -m "feat(ui): implement chat tab with 3-pane layout"
```

---

## Sprint 3: Sessions Tab Implementation

### Task 5: Create Sessions List Widget

**Files:**
- Create: `ui/flutter_ui/lib/features/sessions/sessions_list.dart`
- Create: `ui/flutter_ui/lib/models/session.dart`

- [ ] **Step 1: Create session model**

```dart
import 'package:equatable/equatable.dart';

class Session extends Equatable {
  final String id;
  final String title;
  final DateTime createdAt;
  final DateTime lastActivityAt;
  final Duration duration;
  final int tokenCount;
  final List<String> taskIds;
  final String status;

  const Session({
    required this.id,
    required this.title,
    required this.createdAt,
    required this.lastActivityAt,
    required this.duration,
    required this.tokenCount,
    required this.taskIds,
    required this.status,
  });

  @override
  List<Object?> get props => [
        id,
        title,
        createdAt,
        lastActivityAt,
        duration,
        tokenCount,
        taskIds,
        status,
      ];
}
```

- [ ] **Step 2: Create sessions_list.dart**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/session.dart';

class SessionsList extends StatelessWidget {
  final List<Session> sessions;
  final String? selectedSessionId;
  final ValueChanged<String> onSessionSelected;

  const SessionsList({
    super.key,
    required this.sessions,
    this.selectedSessionId,
    required this.onSessionSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 280,
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withOpacity(0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(
              'sessions',
              style: CyberpunkTypography.headlineMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
          ),
          Expanded(
            child: ListView.builder(
              itemCount: sessions.length,
              itemBuilder: (context, index) {
                final session = sessions[index];
                final isSelected = session.id == selectedSessionId;
                return _buildSessionTile(session, isSelected);
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSessionTile(Session session, bool isSelected) {
    return InkWell(
      onTap: () => onSessionSelected(session.id),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        decoration: BoxDecoration(
          color: isSelected
              ? CyberpunkColors.orangePrimary.withOpacity(0.1)
              : null,
          border: Border(
            left: BorderSide(
              color: isSelected
                  ? CyberpunkColors.orangePrimary
                  : Colors.transparent,
              width: 2,
            ),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              session.title.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: isSelected
                    ? CyberpunkColors.orangePrimary
                    : CyberpunkColors.greenSuccess,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              _formatLastActivity(session.lastActivityAt),
              style: CyberpunkTypography.bodySmall,
            ),
          ],
        ),
      ),
    );
  }

  String _formatLastActivity(DateTime date) {
    final now = DateTime.now();
    final diff = now.difference(date);
    if (diff.inMinutes < 1) return 'just now';
    if (diff.inHours < 1) return '${diff.inMinutes}m ago';
    if (diff.inDays < 1) return '${diff.inHours}h ago';
    return '${diff.inDays}d ago';
  }
}
```

- [ ] **Step 3: Commit sessions list**

```bash
cd ui/flutter_ui
git add lib/models/session.dart lib/features/sessions/sessions_list.dart
git commit -m "feat(ui): add sessions list widget"
```

---

### Task 6: Create Sessions Detail Pane

**Files:**
- Create: `ui/flutter_ui/lib/features/sessions/sessions_detail.dart`

- [ ] **Step 1: Create sessions_detail.dart**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/session.dart';

class SessionsDetailPane extends StatelessWidget {
  final Session session;

  const SessionsDetailPane({super.key, required this.session});

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'session details',
              style: CyberpunkTypography.headlineMedium.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 24),
            _buildDetailRow(
              'title',
              session.title.toLowerCase(),
            ),
            _buildDetailRow(
              'duration',
              _formatDuration(session.duration),
            ),
            _buildDetailRow(
              'tokens',
              '${session.tokenCount}',
            ),
            _buildDetailRow(
              'status',
              session.status.toLowerCase(),
            ),
            const Spacer(),
            _buildTasksSection(),
          ],
        ),
      ),
    );
  }

  Widget _buildDetailRow(String label, String value) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 100,
            child: Text(
              label,
              style: CyberpunkTypography.bodySmall,
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.greenSuccess,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTasksSection() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Divider(color: CyberpunkColors.midGray),
        const SizedBox(height: 8),
        Text(
          'associated tasks',
          style: CyberpunkTypography.label.copyWith(
            color: CyberpunkColors.orangePrimary,
          ),
        ),
        const SizedBox(height: 12),
        ...session.taskIds.map((taskId) => _buildTaskChip(taskId)),
      ],
    );
  }

  Widget _buildTaskChip(String taskId) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(
        color: CyberpunkColors.orangePrimary.withOpacity(0.1),
        border: Border.all(color: CyberpunkColors.orangePrimary),
        borderRadius: BorderRadius.circular(2),
      ),
      child: Text(
        taskId.substring(0, 8).toLowerCase(),
        style: CyberpunkTypography.label.copyWith(
          color: CyberpunkColors.orangePrimary,
        ),
      ),
    );
  }

  String _formatDuration(Duration duration) {
    final hours = duration.inHours;
    final minutes = duration.inMinutes.remainder(60);
    final seconds = duration.inSeconds.remainder(60);
    return '${hours}h ${minutes}m ${seconds}s';
  }
}
```

- [ ] **Step 2: Commit sessions detail pane**

```bash
cd ui/flutter_ui
git add lib/features/sessions/sessions_detail.dart
git commit -m "feat(ui): add sessions detail pane"
```

---

## Sprint 4: Tasks Tab Implementation

### Task 7: Create Tasks List Widget

**Files:**
- Create: `ui/flutter_ui/lib/models/task.dart`
- Modify: `ui/flutter_ui/lib/features/tasks/tasks_tab.dart`

- [ ] **Step 1: Create task and agent models**

```dart
// lib/models/task.dart
import 'package:equatable/equatable.dart';

enum TaskStatus { pending, running, complete, error }

class Task extends Equatable {
  final String id;
  final String title;
  final TaskStatus status;
  final DateTime createdAt;
  final DateTime? lastActivityAt;
  final List<String> agentIds;
  final String? sessionId;

  const Task({
    required this.id,
    required this.title,
    required this.status,
    required this.createdAt,
    this.lastActivityAt,
    required this.agentIds,
    this.sessionId,
  });

  @override
  List<Object?> get props => [
        id,
        title,
        status,
        createdAt,
        lastActivityAt,
        agentIds,
        sessionId,
      ];
}
```

```dart
// lib/models/agent.dart
import 'package:equatable/equatable.dart';

enum AgentStatus { idle, working, complete, error }

class Agent extends Equatable {
  final String id;
  final String name;
  final AgentStatus status;
  final String? currentTaskId;
  final String? transcript;
  final DateTime? lastActiveAt;

  const Agent({
    required this.id,
    required this.name,
    required this.status,
    this.currentTaskId,
    this.transcript,
    this.lastActiveAt,
  });

  @override
  List<Object?> get props => [
        id,
        name,
        status,
        currentTaskId,
        transcript,
        lastActiveAt,
      ];
}
```

- [ ] **Step 2: Update tasks_tab.dart with list/detail layout**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import 'tasks_list.dart';
import 'tasks_detail.dart';
import '../../models/task.dart';

class TasksTab extends StatefulWidget {
  const TasksTab({super.key});

  @override
  State<TasksTab> createState() => _TasksTabState();
}

class _TasksTabState extends State<TasksTab> {
  Task? _selectedTask;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: Row(
        children: [
          TasksList(
            tasks: _getTasks(), // Replace with actual data provider
            selectedTaskId: _selectedTask?.id,
            onTaskSelected: (task) => setState(() => _selectedTask = task),
          ),
          if (_selectedTask != null)
            TasksDetail(task: _selectedTask!),
        ],
      ),
    );
  }

  List<Task> _getTasks() {
    // TODO: Replace with actual Riverpod provider
    return [];
  }
}
```

- [ ] **Step 3: Commit tasks tab structure**

```bash
cd ui/flutter_ui
git add lib/models/task.dart lib/models/agent.dart
git add lib/features/tasks/tasks_tab.dart
git commit -m "feat(ui): add task and agent models, update tasks tab layout"
```

---

### Task 8: Create Tasks Detail Pane with Agent List

**Files:**
- Create: `ui/flutter_ui/lib/features/tasks/tasks_detail.dart`
- Create: `ui/flutter_ui/lib/features/tasks/tasks_list.dart`

- [ ] **Step 1: Create tasks_list.dart**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/task.dart';

class TasksList extends StatelessWidget {
  final List<Task> tasks;
  final String? selectedTaskId;
  final ValueChanged<Task> onTaskSelected;

  const TasksList({
    super.key,
    required this.tasks,
    this.selectedTaskId,
    required this.onTaskSelected,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 320,
      decoration: BoxDecoration(
        border: Border(
          right: BorderSide(
            color: CyberpunkColors.orangeDark.withOpacity(0.3),
            width: 1,
          ),
        ),
      ),
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                Text(
                  'tasks',
                  style: CyberpunkTypography.headlineMedium.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
                const Spacer(),
                IconButton(
                  icon: const Icon(Icons.add, size: 18),
                  color: CyberpunkColors.orangePrimary,
                  onPressed: () {
                    // Create new task
                  },
                ),
              ],
            ),
          ),
          Expanded(
            child: ListView.builder(
              itemCount: tasks.length,
              itemBuilder: (context, index) {
                final task = tasks[index];
                final isSelected = task.id == selectedTaskId;
                return _buildTaskTile(task, isSelected);
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildTaskTile(Task task, bool isSelected) {
    return InkWell(
      onTap: () => onTaskSelected(task),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        decoration: BoxDecoration(
          color: isSelected
              ? CyberpunkColors.orangePrimary.withOpacity(0.1)
              : null,
          border: Border(
            left: BorderSide(
              color: _getStatusColor(task.status),
              width: 2,
            ),
          ),
        ),
        child: Row(
          children: [
            _buildStatusIndicator(task.status),
            const SizedBox(width: 8),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    task.title.toLowerCase(),
                    style: CyberpunkTypography.bodyMedium.copyWith(
                      color: isSelected
                          ? CyberpunkColors.orangePrimary
                          : CyberpunkColors.greenSuccess,
                    ),
                  ),
                  if (task.lastActivityAt != null)
                    Text(
                      _formatLastActivity(task.lastActivityAt!),
                      style: CyberpunkTypography.bodySmall,
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildStatusIndicator(TaskStatus status) {
    final color = _getStatusColor(status);
    return Container(
      width: 8,
      height: 8,
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
      ),
    );
  }

  Color _getStatusColor(TaskStatus status) {
    switch (status) {
      case TaskStatus.pending:
        return CyberpunkColors.yellowWarning;
      case TaskStatus.running:
        return CyberpunkColors.blueInfo;
      case TaskStatus.complete:
        return CyberpunkColors.greenSuccess;
      case TaskStatus.error:
        return CyberpunkColors.redAlert;
    }
  }

  String _formatLastActivity(DateTime date) {
    final now = DateTime.now();
    final diff = now.difference(date);
    if (diff.inMinutes < 1) return 'just now';
    if (diff.inHours < 1) return '${diff.inMinutes}m ago';
    if (diff.inDays < 1) return '${diff.inHours}h ago';
    return '${diff.inDays}d ago';
  }
}
```

- [ ] **Step 2: Create tasks_detail.dart**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/task.dart';
import '../../models/agent.dart';

class TasksDetail extends StatelessWidget {
  final Task task;

  const TasksDetail({super.key, required this.task});

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                _buildStatusIndicator(task.status),
                const SizedBox(width: 8),
                Text(
                  task.title.toLowerCase(),
                  style: CyberpunkTypography.headlineLarge.copyWith(
                    color: CyberpunkColors.orangePrimary,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 24),
            Text(
              'agents',
              style: CyberpunkTypography.label.copyWith(
                color: CyberpunkColors.orangePrimary,
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView.builder(
                itemCount: task.agentIds.length,
                itemBuilder: (context, index) {
                  final agentId = task.agentIds[index];
                  return _buildAgentTile(agentId);
                },
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildAgentTile(String agentId) {
    return InkWell(
      onTap: () {
        // Open agent transcript
      },
      child: Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border.all(color: CyberpunkColors.orangeDark),
          borderRadius: BorderRadius.circular(2),
        ),
        child: Text(
          agentId.toLowerCase(),
          style: CyberpunkTypography.bodyMedium.copyWith(
            color: CyberpunkColors.greenSuccess,
          ),
        ),
      ),
    );
  }

  Widget _buildStatusIndicator(TaskStatus status) {
    final color = _getStatusColor(status);
    return Container(
      width: 10,
      height: 10,
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
      ),
    );
  }

  Color _getStatusColor(TaskStatus status) {
    switch (status) {
      case TaskStatus.pending:
        return CyberpunkColors.yellowWarning;
      case TaskStatus.running:
        return CyberpunkColors.blueInfo;
      case TaskStatus.complete:
        return CyberpunkColors.greenSuccess;
      case TaskStatus.error:
        return CyberpunkColors.redAlert;
    }
  }
}
```

- [ ] **Step 3: Commit tasks detail implementation**

```bash
cd ui/flutter_ui
git add lib/features/tasks/tasks_detail.dart lib/features/tasks/tasks_list.dart
git commit -m "feat(ui): add tasks list and detail pane"
```

---

## Sprint 5: Agents Tab Implementation

### Task 9: Create Agents List Widget

**Files:**
- Create: `ui/flutter_ui/lib/features/agents/agents_list.dart`
- Modify: `ui/flutter_ui/lib/features/agents/agents_tab.dart`

- [ ] **Step 1: Create agents_list.dart**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../models/agent.dart';
import '../../models/task.dart';

class AgentsList extends StatelessWidget {
  final List<Agent> agents;
  final List<Task> tasks;
  final ValueChanged<String>? onAgentSelected;

  const AgentsList({
    super.key,
    required this.agents,
    required this.tasks,
    this.onAgentSelected,
  });

  @override
  Widget build(BuildContext context) {
    final agentsByTask = _groupAgentsByTask(agents, tasks);

    return Container(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'agents',
            style: CyberpunkTypography.headlineMedium.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const SizedBox(height: 16),
          Expanded(
            child: ListView(
              children: [
                ...agentsByTask.entries.map((entry) {
                  final task = entry.key;
                  final taskAgents = entry.value;
                  return _buildTaskGroup(task, taskAgents);
                }),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Map<Task, List<Agent>> _groupAgentsByTask(
      List<Agent> agents, List<Task> tasks) {
    final result = <Task, List<Agent>>{};
    for (final agent in agents) {
      if (agent.currentTaskId != null) {
        final task = tasks.firstWhere(
          (t) => t.id == agent.currentTaskId,
          orElse: () => throw Exception('task not found'),
        );
        result.putIfAbsent(task, () => []).add(agent);
      }
    }
    return result;
  }

  Widget _buildTaskGroup(Task task, List<Agent> agents) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Text(
            task.title.toLowerCase(),
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.greenSuccess,
            ),
          ),
        ),
        ...agents.map((agent) => _buildAgentTile(agent)),
      ],
    );
  }

  Widget _buildAgentTile(Agent agent) {
    return InkWell(
      onTap: () => onAgentSelected?.call(agent.id),
      child: Container(
        margin: const EdgeInsets.only(bottom: 4, left: 16),
        padding: const EdgeInsets.all(8),
        decoration: BoxDecoration(
          color: CyberpunkColors.darkGray,
          border: Border(
            left: BorderSide(
              color: _getStatusColor(agent.status),
              width: 2,
            ),
          ),
        ),
        child: Row(
          children: [
            _buildStatusIndicator(agent.status),
            const SizedBox(width: 8),
            Text(
              agent.name.toLowerCase(),
              style: CyberpunkTypography.bodyMedium.copyWith(
                color: CyberpunkColors.orangeGlow,
              ),
            ),
            const Spacer(),
            Text(
              agent.status.name.toLowerCase(),
              style: CyberpunkTypography.bodySmall.copyWith(
                color: _getStatusColor(agent.status),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildStatusIndicator(AgentStatus status) {
    return Container(
      width: 6,
      height: 6,
      decoration: BoxDecoration(
        color: _getStatusColor(status),
        shape: BoxShape.circle,
      ),
    );
  }

  Color _getStatusColor(AgentStatus status) {
    switch (status) {
      case AgentStatus.idle:
        return Colors.grey;
      case AgentStatus.working:
        return CyberpunkColors.blueInfo;
      case AgentStatus.complete:
        return CyberpunkColors.greenSuccess;
      case AgentStatus.error:
        return CyberpunkColors.redAlert;
    }
  }
}
```

- [ ] **Step 2: Update agents_tab.dart**

```dart
import 'package:flutter/material.dart';
import '../../theme/colors.dart';
import 'agents_list.dart';
import '../../models/agent.dart';
import '../../models/task.dart';

class AgentsTab extends StatelessWidget {
  const AgentsTab({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.black,
      child: AgentsList(
        agents: _getAgents(),
        tasks: _getTasks(),
        onAgentSelected: (agentId) {
          // Open agent transcript
        },
      ),
    );
  }

  List<Agent> _getAgents() {
    // TODO: Replace with Riverpod provider
    return [];
  }

  List<Task> _getTasks() {
    // TODO: Replace with Riverpod provider
    return [];
  }
}
```

- [ ] **Step 3: Commit agents tab implementation**

```bash
cd ui/flutter_ui
git add lib/features/agents/agents_list.dart lib/features/agents/agents_tab.dart
git commit -m "feat(ui): add agents list grouped by task"
```

---

## Sprint 6: Integration and Wiring

### Task 10: Update Home Screen with New Tab Structure

**Files:**
- Modify: `ui/flutter_ui/lib/features/home/home_screen.dart`
- Modify: `ui/flutter_ui/lib/features/home/tab_content.dart`

- [ ] **Step 1: Update HomeTab enum to include chat**

```dart
enum HomeTab { chat, agents, tasks, sessions }
```

- [ ] **Step 2: Update home_screen.dart with top tab bar**

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../theme/cyberpunk_theme.dart';
import '../../theme/colors.dart';
import '../../widgets/tab_bar.dart';
import 'navigation_rail.dart';
import 'tab_content.dart';

enum HomeTab { chat, agents, tasks, sessions }

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  HomeTab _selectedTab = HomeTab.chat;

  final List<String> _tabLabels = ['chat', 'sessions', 'tasks', 'agents'];

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: CyberpunkColors.black,
      body: Container(
        decoration: BoxDecoration(
          gradient: CyberpunkEffects.angularGradient,
        ),
        child: SafeArea(
          child: Column(
            children: [
              // Top tab bar
              OrangeVoidTabBar(
                tabs: _tabLabels,
                selectedIndex: _selectedTab.index,
                onTabSelected: (index) =>
                    setState(() => _selectedTab = HomeTab.values[index]),
              ),
              // Main content
              Expanded(
                child: _buildTabContent(),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildTabContent() {
    return TabContent(
      selectedTab: _selectedTab,
    );
  }
}
```

- [ ] **Step 3: Update tab_content.dart**

```dart
import 'package:flutter/material.dart';
import 'home_screen.dart';
import '../chat/chat_tab.dart';
import '../sessions/sessions_overview_tab.dart';
import '../tasks/tasks_tab.dart';
import '../agents/agents_tab.dart';

class TabContent extends StatelessWidget {
  final HomeTab selectedTab;

  const TabContent({
    super.key,
    required this.selectedTab,
  });

  @override
  Widget build(BuildContext context) {
    switch (selectedTab) {
      case HomeTab.chat:
        return const ChatView(sessionId: 'default');
      case HomeTab.sessions:
        return const SessionsOverviewTab();
      case HomeTab.tasks:
        return const TasksTab();
      case HomeTab.agents:
        return const AgentsTab();
    }
  }
}
```

- [ ] **Step 4: Commit home screen updates**

```bash
cd ui/flutter_ui
git add lib/features/home/home_screen.dart lib/features/home/tab_content.dart
git commit -m "feat(ui): integrate new 4-tab structure with top navigation"
```

---

### Task 11: Wire Up API Integration

**Files:**
- Modify: `ui/flutter_ui/lib/services/api_client.dart`
- Modify: `ui/flutter_ui/lib/providers/providers.dart`

- [ ] **Step 1: Update API client with session/task/agent endpoints**

Add methods for:
- `getSessions()` - GET /api/v1/sessions
- `getSession(String id)` - GET /api/v1/sessions/{id}
- `getTasks()` - GET /api/v1/tasks
- `getTask(String id)` - GET /api/v1/tasks/{id}
- `getAgents()` - GET /api/v1/agents
- `getAgentTranscript(String id)` - GET /api/v1/agents/{id}/transcript

- [ ] **Step 2: Create Riverpod providers**

```dart
// In providers/providers.dart

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/session.dart';
import '../models/task.dart';
import '../models/agent.dart';
import '../services/api_client.dart';

final apiClientProvider = Provider<APIClient>((ref) {
  return APIClient(baseUrl: 'http://localhost:8081');
});

final sessionsProvider = FutureProvider<List<Session>>((ref) async {
  final client = ref.watch(apiClientProvider);
  return client.getSessions();
});

final tasksProvider = FutureProvider<List<Task>>((ref) async {
  final client = ref.watch(apiClientProvider);
  return client.getTasks();
});

final agentsProvider = FutureProvider<List<Agent>>((ref) async {
  final client = ref.watch(apiClientProvider);
  return client.getAgents();
});

final selectedSessionProvider = StateProvider<Session?>((ref) => null);
final selectedTaskProvider = StateProvider<Task?>((ref) => null);
```

- [ ] **Step 3: Commit API integration**

```bash
cd ui/flutter_ui
git add lib/services/api_client.dart lib/providers/providers.dart
git commit -m "feat(ui): add API client methods and Riverpod providers"
```

---

## Testing & Verification

### Task 12: Visual Testing and Polish

**Files:**
- All UI files

- [ ] **Step 1: Run Flutter analyzer**

```bash
cd ui/flutter_ui
flutter analyze
```

- [ ] **Step 2: Run the app and verify all tabs**

```bash
# For web
flutter run -d chrome

# For macOS
flutter run -d macos
```

- [ ] **Step 3: Verify theme consistency**

Check that:
- All text is lowercase
- Orange color scheme is applied consistently
- Tab bar shows correct active state
- Status indicators use correct colors

- [ ] **Step 4: Final commit**

```bash
cd ui/flutter_ui
git add .
git commit -m "chore(ui): polish and final adjustments for ORANGE VOID theme"
```

---

## Plan Complete

**Summary:** This plan updates the Meept Flutter client with:
1. ORANGE VOID theme (orange/black with Source Code Pro font)
2. 4-tab top navigation (Chat, Sessions, Tasks, Agents)
3. 3-pane chat layout with collapsible sidebar
4. Master-detail views for Sessions and Tasks
5. Agent list grouped by task with status indicators
6. Riverpod integration for state management
7. API client wiring for all data models

**Next Steps:** Run `flutter run` to test the updated UI, then connect to the running daemon API for live data.
