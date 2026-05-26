import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';

/// Calendar event model
class CalendarEvent {
  final String id;
  final String summary;
  final String? description;
  final String? location;
  final DateTime start;
  final DateTime end;

  CalendarEvent({
    required this.id,
    required this.summary,
    this.description,
    this.location,
    required this.start,
    required this.end,
  });

  factory CalendarEvent.fromJson(Map<String, dynamic> json) {
    final startVal = json['start'] ?? {};
    final endVal = json['end'] ?? {};
    return CalendarEvent(
      id: json['id'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      description: json['description'] as String?,
      location: json['location'] as String?,
      start: DateTime.tryParse((startVal['dateTime'] as String?) ?? (startVal['date'] as String?) ?? '') ?? DateTime.now(),
      end: DateTime.tryParse((endVal['dateTime'] as String?) ?? (endVal['date'] as String?) ?? '') ?? DateTime.now(),
    );
  }
}

/// Calendar panel - displays upcoming events and allows creating new ones
class CalendarPanel extends ConsumerStatefulWidget {
  const CalendarPanel({super.key});

  @override
  ConsumerState<CalendarPanel> createState() => _CalendarPanelState();
}

class _CalendarPanelState extends ConsumerState<CalendarPanel> {
  List<CalendarEvent> _events = [];
  bool _isLoading = false;
  String? _error;
  bool _showCreateDialog = false;

  @override
  void initState() {
    super.initState();
    _loadEvents();
  }

  Future<void> _loadEvents() async {
    setState(() => _isLoading = true);
    try {
      final client = ref.read(apiClientProvider);
      // Get today's events
      final data = await client.get<Map<String, dynamic>>('/calendar/today');
      final eventsData = data['events'] as List? ?? [];
      if (mounted) {
        setState(() {
          _events = eventsData
              .map((e) => CalendarEvent.fromJson(e as Map<String, dynamic>))
              .toList();
          _isLoading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _isLoading = false;
        });
      }
    }
  }

  void _showCreateEventDialog() {
    showDialog(
      context: context,
      builder: (context) => _CreateEventDialog(onCreate: _createEvent),
    );
  }

  Future<void> _createEvent(String summary, DateTime start, DateTime end, String? description) async {
    Navigator.pop(context);
    try {
      final client = ref.read(apiClientProvider);
      await client.post('/calendar/events', data: {
        'summary': summary,
        'start': start.toIso8601String(),
        'end': end.toIso8601String(),
        if (description != null) 'description': description,
      });
      if (mounted) {
        _loadEvents();
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('event created'),
            backgroundColor: CyberpunkColors.greenSuccess,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('failed to create event: $e'),
            backgroundColor: CyberpunkColors.redAlert,
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      color: CyberpunkColors.darkGray,
      child: Column(
        children: [
          _buildHeader(),
          if (_error != null) _buildErrorBanner(),
          Expanded(child: _buildEventList()),
        ],
      ),
    );
  }

  Widget _buildHeader() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: const BoxDecoration(
        border: Border(
          bottom: BorderSide(color: CyberpunkColors.midGray, width: 1),
        ),
      ),
      child: Row(
        children: [
          const Icon(
            Icons.calendar_today,
            color: CyberpunkColors.orangePrimary,
            size: 18,
          ),
          const SizedBox(width: 8),
          Text(
            'calendar',
            style: CyberpunkTypography.label.copyWith(
              color: CyberpunkColors.orangePrimary,
            ),
          ),
          const Spacer(),
          IconButton(
            icon: const Icon(Icons.add, size: 18),
            onPressed: _showCreateEventDialog,
            tooltip: 'create event',
          ),
          IconButton(
            icon: const Icon(Icons.refresh, size: 16),
            onPressed: _loadEvents,
            tooltip: 'refresh',
          ),
        ],
      ),
    );
  }

  Widget _buildErrorBanner() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      color: const Color(0x80FF3030),
      child: Row(
        children: [
          const Icon(Icons.error_outline, color: Color(0xFFFF6060), size: 14),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              _error!,
              style: const TextStyle(color: Color(0xFFFF6060), fontSize: 10),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          GestureDetector(
            onTap: _loadEvents,
            child: const Icon(Icons.refresh, color: Color(0xFFFF6060), size: 14),
          ),
        ],
      ),
    );
  }

  Widget _buildEventList() {
    if (_isLoading) {
      return const Center(
        child: SizedBox(
          width: 20,
          height: 20,
          child: CircularProgressIndicator(
            strokeWidth: 2,
            valueColor: AlwaysStoppedAnimation<Color>(CyberpunkColors.orangePrimary),
          ),
        ),
      );
    }

    if (_events.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.event_busy, color: CyberpunkColors.midGray, size: 48),
            const SizedBox(height: 8),
            Text(
              'no events today',
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
              ),
            ),
          ],
        ),
      );
    }

    return ListView.builder(
      itemCount: _events.length,
      itemBuilder: (context, index) => _buildEventItem(_events[index]),
    );
  }

  Widget _buildEventItem(CalendarEvent event) {
    final timeFormat = DateFormat('HH:mm');
    final isAllDay = event.start.difference(event.end).inDays == 0 &&
        event.start.hour == 0 && event.start.minute == 0;

    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: CyberpunkColors.black.withValues(alpha: 0.3),
        border: Border.all(
          color: CyberpunkColors.orangePrimary.withValues(alpha: 0.3),
          width: 1,
        ),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                Icons.access_time,
                size: 14,
                color: CyberpunkColors.orangeBright,
              ),
              const SizedBox(width: 6),
              Text(
                isAllDay
                    ? 'all day'
                    : '${timeFormat.format(event.start)} - ${timeFormat.format(event.end)}',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.orangeBright,
                  fontSize: 10,
                  fontFamily: 'SourceCodePro',
                ),
              ),
              const Spacer(),
              if (event.location != null)
                Icon(Icons.location_on, size: 14, color: CyberpunkColors.midGray),
            ],
          ),
          const SizedBox(height: 6),
          Text(
            event.summary,
            style: CyberpunkTypography.bodyMedium.copyWith(
              color: CyberpunkColors.lightGray,
              fontWeight: FontWeight.bold,
            ),
          ),
          if (event.description != null && event.description!.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              event.description!,
              style: CyberpunkTypography.bodySmall.copyWith(
                color: CyberpunkColors.midGray,
                fontSize: 10,
              ),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
          ],
        ],
      ),
    );
  }
}

class _CreateEventDialog extends StatefulWidget {
  final Function(String, DateTime, DateTime, String?) onCreate;

  const _CreateEventDialog({required this.onCreate});

  @override
  State<_CreateEventDialog> createState() => _CreateEventDialogState();
}

class _CreateEventDialogState extends State<_CreateEventDialog> {
  final _summaryController = TextEditingController();
  final _descriptionController = TextEditingController();
  DateTime _startDate = DateTime.now();
  DateTime _endDate = DateTime.now().add(const Duration(hours: 1));

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      backgroundColor: CyberpunkColors.darkGray,
      title: const Text('create event'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _summaryController,
              decoration: const InputDecoration(
                labelText: 'summary',
                border: OutlineInputBorder(),
              ),
              style: CyberpunkTypography.bodySmall,
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _descriptionController,
              decoration: const InputDecoration(
                labelText: 'description',
                border: OutlineInputBorder(),
              ),
              maxLines: 3,
              style: CyberpunkTypography.bodySmall,
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: ElevatedButton.icon(
                    onPressed: () async {
                      final picked = await showDatePicker(
                        context: context,
                        initialDate: _startDate,
                        firstDate: DateTime.now(),
                        lastDate: DateTime.now().add(const Duration(days: 365)),
                      );
                      if (picked != null) {
                        setState(() {
                          final duration = _endDate.difference(_startDate);
                          _startDate = picked;
                          _endDate = picked.add(duration);
                        });
                      }
                    },
                    icon: const Icon(Icons.calendar_today),
                    label: Text(DateFormat('yyyy-MM-dd').format(_startDate)),
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: ElevatedButton.icon(
                    onPressed: () async {
                      final time = await showTimePicker(
                        context: context,
                        initialTime: TimeOfDay.fromDateTime(_startDate),
                      );
                      if (time != null) {
                        setState(() {
                          _startDate = DateTime(
                            _startDate.year,
                            _startDate.month,
                            _startDate.day,
                            time.hour,
                            time.minute,
                          );
                        });
                      }
                    },
                    icon: const Icon(Icons.access_time),
                    label: Text(DateFormat('HH:mm').format(_startDate)),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('cancel'),
        ),
        ElevatedButton(
          onPressed: () {
            if (_summaryController.text.trim().isNotEmpty) {
              widget.onCreate(
                _summaryController.text.trim(),
                _startDate,
                _endDate,
                _descriptionController.text.trim().isEmpty
                    ? null
                    : _descriptionController.text.trim(),
              );
            }
          },
          child: const Text('create'),
        ),
      ],
    );
  }

  @override
  void dispose() {
    _summaryController.dispose();
    _descriptionController.dispose();
    super.dispose();
  }
}
