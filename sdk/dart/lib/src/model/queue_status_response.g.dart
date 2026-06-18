// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'queue_status_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$QueueStatusResponse extends QueueStatusResponse {
  @override
  final int steeringDepth;
  @override
  final int followupDepth;
  @override
  final bool isActive;
  @override
  final int generation;

  factory _$QueueStatusResponse(
          [void Function(QueueStatusResponseBuilder)? updates]) =>
      (QueueStatusResponseBuilder()..update(updates))._build();

  _$QueueStatusResponse._(
      {required this.steeringDepth,
      required this.followupDepth,
      required this.isActive,
      required this.generation})
      : super._();
  @override
  QueueStatusResponse rebuild(
          void Function(QueueStatusResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  QueueStatusResponseBuilder toBuilder() =>
      QueueStatusResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is QueueStatusResponse &&
        steeringDepth == other.steeringDepth &&
        followupDepth == other.followupDepth &&
        isActive == other.isActive &&
        generation == other.generation;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, steeringDepth.hashCode);
    _$hash = $jc(_$hash, followupDepth.hashCode);
    _$hash = $jc(_$hash, isActive.hashCode);
    _$hash = $jc(_$hash, generation.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'QueueStatusResponse')
          ..add('steeringDepth', steeringDepth)
          ..add('followupDepth', followupDepth)
          ..add('isActive', isActive)
          ..add('generation', generation))
        .toString();
  }
}

class QueueStatusResponseBuilder
    implements Builder<QueueStatusResponse, QueueStatusResponseBuilder> {
  _$QueueStatusResponse? _$v;

  int? _steeringDepth;
  int? get steeringDepth => _$this._steeringDepth;
  set steeringDepth(int? steeringDepth) =>
      _$this._steeringDepth = steeringDepth;

  int? _followupDepth;
  int? get followupDepth => _$this._followupDepth;
  set followupDepth(int? followupDepth) =>
      _$this._followupDepth = followupDepth;

  bool? _isActive;
  bool? get isActive => _$this._isActive;
  set isActive(bool? isActive) => _$this._isActive = isActive;

  int? _generation;
  int? get generation => _$this._generation;
  set generation(int? generation) => _$this._generation = generation;

  QueueStatusResponseBuilder() {
    QueueStatusResponse._defaults(this);
  }

  QueueStatusResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _steeringDepth = $v.steeringDepth;
      _followupDepth = $v.followupDepth;
      _isActive = $v.isActive;
      _generation = $v.generation;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(QueueStatusResponse other) {
    _$v = other as _$QueueStatusResponse;
  }

  @override
  void update(void Function(QueueStatusResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  QueueStatusResponse build() => _build();

  _$QueueStatusResponse _build() {
    final _$result = _$v ??
        _$QueueStatusResponse._(
          steeringDepth: BuiltValueNullFieldError.checkNotNull(
              steeringDepth, r'QueueStatusResponse', 'steeringDepth'),
          followupDepth: BuiltValueNullFieldError.checkNotNull(
              followupDepth, r'QueueStatusResponse', 'followupDepth'),
          isActive: BuiltValueNullFieldError.checkNotNull(
              isActive, r'QueueStatusResponse', 'isActive'),
          generation: BuiltValueNullFieldError.checkNotNull(
              generation, r'QueueStatusResponse', 'generation'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
