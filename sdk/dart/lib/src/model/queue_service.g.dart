// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'queue_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$QueueService extends QueueService {
  @override
  final JsonObject? q;

  factory _$QueueService([void Function(QueueServiceBuilder)? updates]) =>
      (QueueServiceBuilder()..update(updates))._build();

  _$QueueService._({this.q}) : super._();
  @override
  QueueService rebuild(void Function(QueueServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  QueueServiceBuilder toBuilder() => QueueServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is QueueService && q == other.q;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, q.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'QueueService')..add('q', q))
        .toString();
  }
}

class QueueServiceBuilder
    implements Builder<QueueService, QueueServiceBuilder> {
  _$QueueService? _$v;

  JsonObject? _q;
  JsonObject? get q => _$this._q;
  set q(JsonObject? q) => _$this._q = q;

  QueueServiceBuilder() {
    QueueService._defaults(this);
  }

  QueueServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _q = $v.q;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(QueueService other) {
    _$v = other as _$QueueService;
  }

  @override
  void update(void Function(QueueServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  QueueService build() => _build();

  _$QueueService _build() {
    final _$result = _$v ??
        _$QueueService._(
          q: q,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
