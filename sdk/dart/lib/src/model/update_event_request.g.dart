// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'update_event_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$UpdateEventRequest extends UpdateEventRequest {
  @override
  final String id;
  @override
  final String? summary;
  @override
  final String? description;
  @override
  final String? location;
  @override
  final String? start;
  @override
  final String? end;

  factory _$UpdateEventRequest(
          [void Function(UpdateEventRequestBuilder)? updates]) =>
      (UpdateEventRequestBuilder()..update(updates))._build();

  _$UpdateEventRequest._(
      {required this.id,
      this.summary,
      this.description,
      this.location,
      this.start,
      this.end})
      : super._();
  @override
  UpdateEventRequest rebuild(
          void Function(UpdateEventRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  UpdateEventRequestBuilder toBuilder() =>
      UpdateEventRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is UpdateEventRequest &&
        id == other.id &&
        summary == other.summary &&
        description == other.description &&
        location == other.location &&
        start == other.start &&
        end == other.end;
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
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UpdateEventRequest')
          ..add('id', id)
          ..add('summary', summary)
          ..add('description', description)
          ..add('location', location)
          ..add('start', start)
          ..add('end', end))
        .toString();
  }
}

class UpdateEventRequestBuilder
    implements Builder<UpdateEventRequest, UpdateEventRequestBuilder> {
  _$UpdateEventRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _summary;
  String? get summary => _$this._summary;
  set summary(String? summary) =>
      _$this._summary = summary;

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
  set start(String? start) =>
      _$this._start = start;

  String? _end;
  String? get end => _$this._end;
  set end(String? end) =>
      _$this._end = end;

  UpdateEventRequestBuilder() {
    UpdateEventRequest._defaults(this);
  }

  UpdateEventRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _summary = $v.summary;
      _description = $v.description;
      _location = $v.location;
      _start = $v.start;
      _end = $v.end;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(UpdateEventRequest other) {
    _$v = other as _$UpdateEventRequest;
  }

  @override
  void update(void Function(UpdateEventRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  UpdateEventRequest build() => _build();

  _$UpdateEventRequest _build() {
    final _$result = _$v ??
        _$UpdateEventRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'UpdateEventRequest', 'id'),
          summary: summary,
          description: description,
          location: location,
          start: start,
          end: end,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
