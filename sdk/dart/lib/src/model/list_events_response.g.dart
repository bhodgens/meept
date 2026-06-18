// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_events_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListEventsResponse extends ListEventsResponse {
  @override
  final BuiltList<String>? events;
  @override
  final int count;

  factory _$ListEventsResponse(
          [void Function(ListEventsResponseBuilder)? updates]) =>
      (ListEventsResponseBuilder()..update(updates))._build();

  _$ListEventsResponse._({this.events, required this.count}) : super._();
  @override
  ListEventsResponse rebuild(
          void Function(ListEventsResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ListEventsResponseBuilder toBuilder() =>
      ListEventsResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ListEventsResponse &&
        events == other.events &&
        count == other.count;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, events.hashCode);
    _$hash = $jc(_$hash, count.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListEventsResponse')
          ..add('events', events)
          ..add('count', count))
        .toString();
  }
}

class ListEventsResponseBuilder
    implements Builder<ListEventsResponse, ListEventsResponseBuilder> {
  _$ListEventsResponse? _$v;

  ListBuilder<String>? _events;
  ListBuilder<String> get events => _$this._events ??= ListBuilder<String>();
  set events(ListBuilder<String>? events) => _$this._events = events;

  int? _count;
  int? get count => _$this._count;
  set count(int? count) => _$this._count = count;

  ListEventsResponseBuilder() {
    ListEventsResponse._defaults(this);
  }

  ListEventsResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _events = $v.events?.toBuilder();
      _count = $v.count;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ListEventsResponse other) {
    _$v = other as _$ListEventsResponse;
  }

  @override
  void update(void Function(ListEventsResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ListEventsResponse build() => _build();

  _$ListEventsResponse _build() {
    _$ListEventsResponse _$result;
    try {
      _$result = _$v ??
          _$ListEventsResponse._(
            events: _events?.build(),
            count: BuiltValueNullFieldError.checkNotNull(
                count, r'ListEventsResponse', 'count'),
          );
    } catch (_) {
      late String _$failedField;
      try {
        _$failedField = 'events';
        _events?.build();
      } catch (e) {
        throw BuiltValueNestedFieldError(
            r'ListEventsResponse', _$failedField, e.toString());
      }
      rethrow;
    }
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
