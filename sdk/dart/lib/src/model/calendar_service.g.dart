// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'calendar_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$CalendarService extends CalendarService {
  @override
  final JsonObject? client;

  factory _$CalendarService([void Function(CalendarServiceBuilder)? updates]) =>
      (CalendarServiceBuilder()..update(updates))._build();

  _$CalendarService._({this.client}) : super._();
  @override
  CalendarService rebuild(void Function(CalendarServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  CalendarServiceBuilder toBuilder() => CalendarServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is CalendarService && client == other.client;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, client.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'CalendarService')
          ..add('client', client))
        .toString();
  }
}

class CalendarServiceBuilder
    implements Builder<CalendarService, CalendarServiceBuilder> {
  _$CalendarService? _$v;

  JsonObject? _client;
  JsonObject? get client => _$this._client;
  set client(JsonObject? client) => _$this._client = client;

  CalendarServiceBuilder() {
    CalendarService._defaults(this);
  }

  CalendarServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _client = $v.client;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(CalendarService other) {
    _$v = other as _$CalendarService;
  }

  @override
  void update(void Function(CalendarServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  CalendarService build() => _build();

  _$CalendarService _build() {
    final _$result = _$v ??
        _$CalendarService._(
          client: client,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
