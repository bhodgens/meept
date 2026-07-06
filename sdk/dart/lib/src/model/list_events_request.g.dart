// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_events_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListEventsRequest extends ListEventsRequest {
  @override
  final String? timeMin;
  @override
  final String? timeMax;
  @override
  final int? maxResults;

  factory _$ListEventsRequest(
          [void Function(ListEventsRequestBuilder)? updates]) =>
      (ListEventsRequestBuilder()..update(updates))._build();

  _$ListEventsRequest._(
      {this.timeMin,
      this.timeMax,
      this.maxResults})
      : super._();
  @override
  ListEventsRequest rebuild(void Function(ListEventsRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ListEventsRequestBuilder toBuilder() =>
      ListEventsRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ListEventsRequest &&
        timeMin == other.timeMin &&
        timeMax == other.timeMax &&
        maxResults == other.maxResults;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, timeMin.hashCode);
    _$hash = $jc(_$hash, timeMax.hashCode);
    _$hash = $jc(_$hash, maxResults.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListEventsRequest')
          ..add('timeMin', timeMin)
          ..add('timeMax', timeMax)
          ..add('maxResults', maxResults))
        .toString();
  }
}

class ListEventsRequestBuilder
    implements Builder<ListEventsRequest, ListEventsRequestBuilder> {
  _$ListEventsRequest? _$v;

  String? _timeMin;
  String? get timeMin => _$this._timeMin;
  set timeMin(String? timeMin) =>
      _$this._timeMin = timeMin;

  String? _timeMax;
  String? get timeMax => _$this._timeMax;
  set timeMax(String? timeMax) =>
      _$this._timeMax = timeMax;

  int? _maxResults;
  int? get maxResults => _$this._maxResults;
  set maxResults(int? maxResults) =>
      _$this._maxResults = maxResults;

  ListEventsRequestBuilder() {
    ListEventsRequest._defaults(this);
  }

  ListEventsRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _timeMin = $v.timeMin;
      _timeMax = $v.timeMax;
      _maxResults = $v.maxResults;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ListEventsRequest other) {
    _$v = other as _$ListEventsRequest;
  }

  @override
  void update(void Function(ListEventsRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ListEventsRequest build() => _build();

  _$ListEventsRequest _build() {
    final _$result = _$v ??
        _$ListEventsRequest._(
          timeMin: timeMin,
          timeMax: timeMax,
          maxResults: maxResults,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
