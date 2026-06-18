// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'create_event_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CreateEventRequest extends CreateEventRequest {
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
  final String? attendeesCommaOmitempty;

  factory _$CreateEventRequest(
          [void Function(CreateEventRequestBuilder)? updates]) =>
      (CreateEventRequestBuilder()..update(updates))._build();

  _$CreateEventRequest._(
      {required this.summary,
      this.descriptionCommaOmitempty,
      this.locationCommaOmitempty,
      required this.start,
      required this.end,
      this.attendeesCommaOmitempty})
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
        descriptionCommaOmitempty == other.descriptionCommaOmitempty &&
        locationCommaOmitempty == other.locationCommaOmitempty &&
        start == other.start &&
        end == other.end &&
        attendeesCommaOmitempty == other.attendeesCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, summary.hashCode);
    _$hash = $jc(_$hash, descriptionCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, locationCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, start.hashCode);
    _$hash = $jc(_$hash, end.hashCode);
    _$hash = $jc(_$hash, attendeesCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CreateEventRequest')
          ..add('summary', summary)
          ..add('descriptionCommaOmitempty', descriptionCommaOmitempty)
          ..add('locationCommaOmitempty', locationCommaOmitempty)
          ..add('start', start)
          ..add('end', end)
          ..add('attendeesCommaOmitempty', attendeesCommaOmitempty))
        .toString();
  }
}

class CreateEventRequestBuilder
    implements Builder<CreateEventRequest, CreateEventRequestBuilder> {
  _$CreateEventRequest? _$v;

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

  String? _attendeesCommaOmitempty;
  String? get attendeesCommaOmitempty => _$this._attendeesCommaOmitempty;
  set attendeesCommaOmitempty(String? attendeesCommaOmitempty) =>
      _$this._attendeesCommaOmitempty = attendeesCommaOmitempty;

  CreateEventRequestBuilder() {
    CreateEventRequest._defaults(this);
  }

  CreateEventRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _summary = $v.summary;
      _descriptionCommaOmitempty = $v.descriptionCommaOmitempty;
      _locationCommaOmitempty = $v.locationCommaOmitempty;
      _start = $v.start;
      _end = $v.end;
      _attendeesCommaOmitempty = $v.attendeesCommaOmitempty;
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
          descriptionCommaOmitempty: descriptionCommaOmitempty,
          locationCommaOmitempty: locationCommaOmitempty,
          start: BuiltValueNullFieldError.checkNotNull(
              start, r'CreateEventRequest', 'start'),
          end: BuiltValueNullFieldError.checkNotNull(
              end, r'CreateEventRequest', 'end'),
          attendeesCommaOmitempty: attendeesCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
