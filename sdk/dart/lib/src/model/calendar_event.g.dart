// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'calendar_event.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CalendarEvent extends CalendarEvent {
  @override
  final String id;
  @override
  final String summary;
  @override
  final String? descriptionCommaOmitempty;
  @override
  final String? locationCommaOmitempty;
  @override
  final String start;
  @override
  final String end;
  @override
  final bool allDay;
  @override
  final String? statusCommaOmitempty;
  @override
  final String? htmlLinkCommaOmitempty;
  @override
  final BuiltList<String>? attendeesCommaOmitempty;

  factory _$CalendarEvent([void Function(CalendarEventBuilder)? updates]) =>
      (CalendarEventBuilder()..update(updates))._build();

  _$CalendarEvent._(
      {required this.id,
      required this.summary,
      this.descriptionCommaOmitempty,
      this.locationCommaOmitempty,
      required this.start,
      required this.end,
      required this.allDay,
      this.statusCommaOmitempty,
      this.htmlLinkCommaOmitempty,
      this.attendeesCommaOmitempty})
      : super._();
  @override
  CalendarEvent rebuild(void Function(CalendarEventBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CalendarEventBuilder toBuilder() => CalendarEventBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CalendarEvent &&
        id == other.id &&
        summary == other.summary &&
        descriptionCommaOmitempty == other.descriptionCommaOmitempty &&
        locationCommaOmitempty == other.locationCommaOmitempty &&
        start == other.start &&
        end == other.end &&
        allDay == other.allDay &&
        statusCommaOmitempty == other.statusCommaOmitempty &&
        htmlLinkCommaOmitempty == other.htmlLinkCommaOmitempty &&
        attendeesCommaOmitempty == other.attendeesCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, summary.hashCode);
    _$hash = $jc(_$hash, descriptionCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, locationCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, start.hashCode);
    _$hash = $jc(_$hash, end.hashCode);
    _$hash = $jc(_$hash, allDay.hashCode);
    _$hash = $jc(_$hash, statusCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, htmlLinkCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, attendeesCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CalendarEvent')
          ..add('id', id)
          ..add('summary', summary)
          ..add('descriptionCommaOmitempty', descriptionCommaOmitempty)
          ..add('locationCommaOmitempty', locationCommaOmitempty)
          ..add('start', start)
          ..add('end', end)
          ..add('allDay', allDay)
          ..add('statusCommaOmitempty', statusCommaOmitempty)
          ..add('htmlLinkCommaOmitempty', htmlLinkCommaOmitempty)
          ..add('attendeesCommaOmitempty', attendeesCommaOmitempty))
        .toString();
  }
}

class CalendarEventBuilder
    implements Builder<CalendarEvent, CalendarEventBuilder> {
  _$CalendarEvent? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _summary;
  String? get summary => _$this._summary;
  set summary(String? summary) => _$this._summary = summary;

  String? _descriptionCommaOmitempty;
  String? get descriptionCommaOmitempty => _$this._descriptionCommaOmitempty;
  set descriptionCommaOmitempty(String? descriptionCommaOmitempty) =>
      _$this._descriptionCommaOmitempty = descriptionCommaOmitempty;

  String? _locationCommaOmitempty;
  String? get locationCommaOmitempty => _$this._locationCommaOmitempty;
  set locationCommaOmitempty(String? locationCommaOmitempty) =>
      _$this._locationCommaOmitempty = locationCommaOmitempty;

  String? _start;
  String? get start => _$this._start;
  set start(String? start) => _$this._start = start;

  String? _end;
  String? get end => _$this._end;
  set end(String? end) => _$this._end = end;

  bool? _allDay;
  bool? get allDay => _$this._allDay;
  set allDay(bool? allDay) => _$this._allDay = allDay;

  String? _statusCommaOmitempty;
  String? get statusCommaOmitempty => _$this._statusCommaOmitempty;
  set statusCommaOmitempty(String? statusCommaOmitempty) =>
      _$this._statusCommaOmitempty = statusCommaOmitempty;

  String? _htmlLinkCommaOmitempty;
  String? get htmlLinkCommaOmitempty => _$this._htmlLinkCommaOmitempty;
  set htmlLinkCommaOmitempty(String? htmlLinkCommaOmitempty) =>
      _$this._htmlLinkCommaOmitempty = htmlLinkCommaOmitempty;

  ListBuilder<String>? _attendeesCommaOmitempty;
  ListBuilder<String> get attendeesCommaOmitempty =>
      _$this._attendeesCommaOmitempty ??= ListBuilder<String>();
  set attendeesCommaOmitempty(ListBuilder<String>? attendeesCommaOmitempty) =>
      _$this._attendeesCommaOmitempty = attendeesCommaOmitempty;

  CalendarEventBuilder() {
    CalendarEvent._defaults(this);
  }

  CalendarEventBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _summary = $v.summary;
      _descriptionCommaOmitempty = $v.descriptionCommaOmitempty;
      _locationCommaOmitempty = $v.locationCommaOmitempty;
      _start = $v.start;
      _end = $v.end;
      _allDay = $v.allDay;
      _statusCommaOmitempty = $v.statusCommaOmitempty;
      _htmlLinkCommaOmitempty = $v.htmlLinkCommaOmitempty;
      _attendeesCommaOmitempty = $v.attendeesCommaOmitempty?.toBuilder();
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CalendarEvent other) {
    _$v = other as _$CalendarEvent;
  }

  @override
  void update(void Function(CalendarEventBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CalendarEvent build() => _build();

  _$CalendarEvent _build() {
    _$CalendarEvent _$result;
    try {
      _$result = _$v ??
          _$CalendarEvent._(
            id: BuiltValueNullFieldError.checkNotNull(
                id, r'CalendarEvent', 'id'),
            summary: BuiltValueNullFieldError.checkNotNull(
                summary, r'CalendarEvent', 'summary'),
            descriptionCommaOmitempty: descriptionCommaOmitempty,
            locationCommaOmitempty: locationCommaOmitempty,
            start: BuiltValueNullFieldError.checkNotNull(
                start, r'CalendarEvent', 'start'),
            end: BuiltValueNullFieldError.checkNotNull(
                end, r'CalendarEvent', 'end'),
            allDay: BuiltValueNullFieldError.checkNotNull(
                allDay, r'CalendarEvent', 'allDay'),
            statusCommaOmitempty: statusCommaOmitempty,
            htmlLinkCommaOmitempty: htmlLinkCommaOmitempty,
            attendeesCommaOmitempty: _attendeesCommaOmitempty?.build(),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'attendeesCommaOmitempty';
        _attendeesCommaOmitempty?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'CalendarEvent', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
