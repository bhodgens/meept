// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_event_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreateEventRequest extends CreateEventRequest {
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
  final String? attendees;

  factory _$CreateEventRequest(
          [void Function(CreateEventRequestBuilder)? updates]) =>
      (CreateEventRequestBuilder()..update(updates))._build();

  _$CreateEventRequest._(
      {required this.summary,
      this.description,
      this.location,
      required this.start,
      required this.end,
      this.attendees})
      : super._();
  @override
  CreateEventRequest rebuild(
          void Function(CreateEventRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CreateEventRequestBuilder toBuilder() =>
      CreateEventRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CreateEventRequest &&
        summary == other.summary &&
        description == other.description &&
        location == other.location &&
        start == other.start &&
        end == other.end &&
        attendees == other.attendees;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, summary.hashCode);
    _$hash = $jc(_$hash, description.hashCode);
    _$hash = $jc(_$hash, location.hashCode);
    _$hash = $jc(_$hash, start.hashCode);
    _$hash = $jc(_$hash, end.hashCode);
    _$hash = $jc(_$hash, attendees.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreateEventRequest')
          ..add('summary', summary)
          ..add('description', description)
          ..add('location', location)
          ..add('start', start)
          ..add('end', end)
          ..add('attendees', attendees))
        .toString();
  }
}

class CreateEventRequestBuilder
    implements Builder<CreateEventRequest, CreateEventRequestBuilder> {
  _$CreateEventRequest? _$v;

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

  String? _attendees;
  String? get attendees => _$this._attendees;
  set attendees(String? attendees) =>
      _$this._attendees = attendees;

  CreateEventRequestBuilder() {
    CreateEventRequest._defaults(this);
  }

  CreateEventRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _summary = $v.summary;
      _description = $v.description;
      _location = $v.location;
      _start = $v.start;
      _end = $v.end;
      _attendees = $v.attendees;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CreateEventRequest other) {
    _$v = other as _$CreateEventRequest;
  }

  @override
  void update(void Function(CreateEventRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CreateEventRequest build() => _build();

  _$CreateEventRequest _build() {
    final _$result = _$v ??
        _$CreateEventRequest._(
          summary: BuiltValueNullFieldError.checkNotNull(
              summary, r'CreateEventRequest', 'summary'),
          description: description,
          location: location,
          start: BuiltValueNullFieldError.checkNotNull(
              start, r'CreateEventRequest', 'start'),
          end: BuiltValueNullFieldError.checkNotNull(
              end, r'CreateEventRequest', 'end'),
          attendees: attendees,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
