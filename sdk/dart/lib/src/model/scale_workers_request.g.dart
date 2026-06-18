// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'scale_workers_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ScaleWorkersRequest extends ScaleWorkersRequest {
  @override
  final int desiredCount;

  factory _$ScaleWorkersRequest(
          [void Function(ScaleWorkersRequestBuilder)? updates]) =>
      (ScaleWorkersRequestBuilder()..update(updates))._build();

  _$ScaleWorkersRequest._({required this.desiredCount}) : super._();
  @override
  ScaleWorkersRequest rebuild(
          void Function(ScaleWorkersRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ScaleWorkersRequestBuilder toBuilder() =>
      ScaleWorkersRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ScaleWorkersRequest && desiredCount == other.desiredCount;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, desiredCount.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ScaleWorkersRequest')
          ..add('desiredCount', desiredCount))
        .toString();
  }
}

class ScaleWorkersRequestBuilder
    implements Builder<ScaleWorkersRequest, ScaleWorkersRequestBuilder> {
  _$ScaleWorkersRequest? _$v;

  int? _desiredCount;
  int? get desiredCount => _$this._desiredCount;
  set desiredCount(int? desiredCount) => _$this._desiredCount = desiredCount;

  ScaleWorkersRequestBuilder() {
    ScaleWorkersRequest._defaults(this);
  }

  ScaleWorkersRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _desiredCount = $v.desiredCount;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ScaleWorkersRequest other) {
    _$v = other as _$ScaleWorkersRequest;
  }

  @override
  void update(void Function(ScaleWorkersRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ScaleWorkersRequest build() => _build();

  _$ScaleWorkersRequest _build() {
    final _$result = _$v ??
        _$ScaleWorkersRequest._(
          desiredCount: BuiltValueNullFieldError.checkNotNull(
              desiredCount, r'ScaleWorkersRequest', 'desiredCount'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
