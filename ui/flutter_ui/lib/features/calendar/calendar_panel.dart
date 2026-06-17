import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';
import '../../models/api_models.dart';
import '../../theme/colors.dart';
import '../../theme/typography.dart';
import '../../providers/providers.dart';
import '../../widgets/error_banner.dart';

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
      final data = await client.getCalendarToday();
      final eventsData = data['events'] as List? ?? [];
      if (mounted) {
        setState(() {
          _events = eventsData
              .map((e) => CalendarEvent.fromJson(e as Map<String, dynamic>))
              .toList();
          _isLoading = false;
          // Clear stale error on successful retry so the error banner
          // does not persist after the issue is resolved.
          _error = null;
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
    try {
      final client = ref.read(apiClientProvider);
      await client.createCalendarEvent(
        summary: summary,
        start: start,
        end: end,
        description: description,
      );
      if (mounted) {
        Navigator.pop(context);
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
          if (_error != null)
            ErrorBanner(message: _error!, onDismiss: _loadEvents),
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
            const Icon(Icons.event_busy, color: CyberpunkColors.midGray, size: 48),
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
    final duration = event.end.difference(event.start);
    final isAllDay = event.start.hour == 0 &&
        event.start.minute == 0 &&
        event.end.hour == 0 &&
        event.end.minute == 0 &&
        duration.inHours >= 23;

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
              const Icon(
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
                const Icon(Icons.location_on, size: 14, color: CyberpunkColors.midGray),
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

  /// Ensures [_endDate] is strictly greater than [_startDate].
  /// If they are equal or [_endDate] is earlier, bumps [_endDate] to
  /// [_startDate] + 1 minute. Called after every date/time mutation.
  void _clampEndDate() {
    if (!_endDate.isAfter(_startDate)) {
      _endDate = _startDate.add(const Duration(minutes: 1));
    }
  }

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
            Align(
              alignment: Alignment.centerLeft,
              child: Text(
                'start',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                  fontSize: 10,
                ),
              ),
            ),
            const SizedBox(height: 4),
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
                          // Preserve the time component and duration delta.
                          final duration = _endDate.difference(_startDate);
                          _startDate = DateTime(
                            picked.year,
                            picked.month,
                            picked.day,
                            _startDate.hour,
                            _startDate.minute,
                          );
                          _endDate = _startDate.add(duration);
                          _clampEndDate();
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
                          final newStart = DateTime(
                            _startDate.year,
                            _startDate.month,
                            _startDate.day,
                            time.hour,
                            time.minute,
                          );
                          // Preserve positive duration; collapse to +1m if needed.
                          final duration = _endDate.difference(_startDate);
                          _startDate = newStart;
                          if (duration.inMinutes < 1) {
                            _endDate = newStart.add(const Duration(minutes: 1));
                          } else {
                            _endDate = newStart.add(duration);
                          }
                          _clampEndDate();
                        });
                      }
                    },
                    icon: const Icon(Icons.access_time),
                    label: Text(DateFormat('HH:mm').format(_startDate)),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerLeft,
              child: Text(
                'end',
                style: CyberpunkTypography.bodySmall.copyWith(
                  color: CyberpunkColors.lightGray,
                  fontSize: 10,
                ),
              ),
            ),
            const SizedBox(height: 4),
            Row(
              children: [
                Expanded(
                  child: ElevatedButton.icon(
                    onPressed: () async {
                      final picked = await showDatePicker(
                        context: context,
                        initialDate: _endDate.isAfter(_startDate)
                            ? _endDate
                            : _startDate.add(const Duration(minutes: 1)),
                        firstDate: _startDate,
                        lastDate: _startDate.add(const Duration(days: 365)),
                      );
                      if (picked != null) {
                        setState(() {
                          _endDate = DateTime(
                            picked.year,
                            picked.month,
                            picked.day,
                            _endDate.hour,
                            _endDate.minute,
                          );
                          _clampEndDate();
                        });
                      }
                    },
                    icon: const Icon(Icons.event),
                    label: Text(DateFormat('yyyy-MM-dd').format(_endDate)),
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: ElevatedButton.icon(
                    onPressed: () async {
                      final time = await showTimePicker(
                        context: context,
                        initialTime: TimeOfDay.fromDateTime(_endDate),
                      );
                      if (time != null) {
                        setState(() {
                          _endDate = DateTime(
                            _endDate.year,
                            _endDate.month,
                            _endDate.day,
                            time.hour,
                            time.minute,
                          );
                          _clampEndDate();
                        });
                      }
                    },
                    icon: const Icon(Icons.access_time),
                    label: Text(DateFormat('HH:mm').format(_endDate)),
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
