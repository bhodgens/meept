// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_events_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListEventsRequest extends ListEventsRequest {
  @override
  final String? timeMinCommaOmitempty;
  @override
  final String? timeMaxCommaOmitempty;
  @override
  final int? maxResultsCommaOmitempty;

  factory _$ListEventsRequest(
          [void Function(ListEventsRequestBuilder)? updates]) =>
      (ListEventsRequestBuilder()..update(updates))._build();

  _$ListEventsRequest._(
      {this.timeMinCommaOmitempty,
      this.timeMaxCommaOmitempty,
      this.maxResultsCommaOmitempty})
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
        timeMinCommaOmitempty == other.timeMinCommaOmitempty &&
        timeMaxCommaOmitempty == other.timeMaxCommaOmitempty &&
        maxResultsCommaOmitempty == other.maxResultsCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, timeMinCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, timeMaxCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, maxResultsCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ListEventsRequest')
          ..add('timeMinCommaOmitempty', timeMinCommaOmitempty)
          ..add('timeMaxCommaOmitempty', timeMaxCommaOmitempty)
          ..add('maxResultsCommaOmitempty', maxResultsCommaOmitempty))
        .toString();
  }
}

class ListEventsRequestBuilder
    implements Builder<ListEventsRequest, ListEventsRequestBuilder> {
  _$ListEventsRequest? _$v;

  String? _timeMinCommaOmitempty;
  String? get timeMinCommaOmitempty => _$this._timeMinCommaOmitempty;
  set timeMinCommaOmitempty(String? timeMinCommaOmitempty) =>
      _$this._timeMinCommaOmitempty = timeMinCommaOmitempty;

  String? _timeMaxCommaOmitempty;
  String? get timeMaxCommaOmitempty => _$this._timeMaxCommaOmitempty;
  set timeMaxCommaOmitempty(String? timeMaxCommaOmitempty) =>
      _$this._timeMaxCommaOmitempty = timeMaxCommaOmitempty;

  int? _maxResultsCommaOmitempty;
  int? get maxResultsCommaOmitempty => _$this._maxResultsCommaOmitempty;
  set maxResultsCommaOmitempty(int? maxResultsCommaOmitempty) =>
      _$this._maxResultsCommaOmitempty = maxResultsCommaOmitempty;

  ListEventsRequestBuilder() {
    ListEventsRequest._defaults(this);
  }

  ListEventsRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _timeMinCommaOmitempty = $v.timeMinCommaOmitempty;
      _timeMaxCommaOmitempty = $v.timeMaxCommaOmitempty;
      _maxResultsCommaOmitempty = $v.maxResultsCommaOmitempty;
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
          timeMinCommaOmitempty: timeMinCommaOmitempty,
          timeMaxCommaOmitempty: timeMaxCommaOmitempty,
          maxResultsCommaOmitempty: maxResultsCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
