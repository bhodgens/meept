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
  final String? description;
  @override
  final String? location;
  @override
  final String start;
  @override
  final String end;
  @override
  final bool allDay;
  @override
  final String? status;
  @override
  final String? htmlLink;
  @override
  final BuiltList<String>? attendees;

  factory _$CalendarEvent([void Function(CalendarEventBuilder)? updates]) =>
      (CalendarEventBuilder()..update(updates))._build();

  _$CalendarEvent._(
      {required this.id,
      required this.summary,
      this.description,
      this.location,
      required this.start,
      required this.end,
      required this.allDay,
      this.status,
      this.htmlLink,
      this.attendees})
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
        description == other.description &&
        location == other.location &&
        start == other.start &&
        end == other.end &&
        allDay == other.allDay &&
        status == other.status &&
        htmlLink == other.htmlLink &&
        attendees == other.attendees;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, summary.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, location.hashCode);
    _$hash = $jc(_$hash, start.hashCode);
    _$hash = $jc(_$hash, end.hashCode);
    _$hash = $jc(_$hash, allDay.hashCode);
    _$hash = $jc(_$hash, status.hashCode);
    _$hash = $jc(_$hash, htmlLink.hashCode);
    _$hash = $jc(_$hash, attendees.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CalendarEvent')
          ..add('id', id)
          ..add('summary', summary)
          ..add('description', description)
          ..add('location', location)
          ..add('start', start)
          ..add('end', end)
          ..add('allDay', allDay)
          ..add('status', status)
          ..add('htmlLink', htmlLink)
          ..add('attendees', attendees))
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

  String? _description;
  String? get description => _$this._description;
  set description(String? description) =>
      _$this._description = description;

  String? _location;
  String? get location => _$this._location;
  set location(String? location) =>
      _$this._location = location;

  String? _start;
  String? get start => _$this._start;
  set start(String? start) => _$this._start = start;

  String? _end;
  String? get end => _$this._end;
  set end(String? end) => _$this._end = end;

  bool? _allDay;
  bool? get allDay => _$this._allDay;
  set allDay(bool? allDay) => _$this._allDay = allDay;

  String? _status;
  String? get status => _$this._status;
  set status(String? status) =>
      _$this._status = status;

  String? _htmlLink;
  String? get htmlLink => _$this._htmlLink;
  set htmlLink(String? htmlLink) =>
      _$this._htmlLink = htmlLink;

  ListBuilder<String>? _attendees;
  ListBuilder<String> get attendees =>
      _$this._attendees ??= ListBuilder<String>();
  set attendees(ListBuilder<String>? attendees) =>
      _$this._attendees = attendees;

  CalendarEventBuilder() {
    CalendarEvent._defaults(this);
  }

  CalendarEventBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _summary = $v.summary;
      _description = $v.description;
      _location = $v.location;
      _start = $v.start;
      _end = $v.end;
      _allDay = $v.allDay;
      _status = $v.status;
      _htmlLink = $v.htmlLink;
      _attendees = $v.attendees?.toBuilder();
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
            description: description,
            location: location,
            start: BuiltValueNullFieldError.checkNotNull(
                start, r'CalendarEvent', 'start'),
            end: BuiltValueNullFieldError.checkNotNull(
                end, r'CalendarEvent', 'end'),
            allDay: BuiltValueNullFieldError.checkNotNull(
                allDay, r'CalendarEvent', 'allDay'),
            status: status,
            htmlLink: htmlLink,
            attendees: _attendees?.build(),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'attendees';
        _attendees?.build();
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
