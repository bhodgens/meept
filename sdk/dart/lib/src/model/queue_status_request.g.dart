// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'queue_status_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$QueueStatusRequest extends QueueStatusRequest {
  @override
  final String conversationId;

  factory _$QueueStatusRequest(
          [void Function(QueueStatusRequestBuilder)? updates]) =>
      (QueueStatusRequestBuilder()..update(updates))._build();

  _$QueueStatusRequest._({required this.conversationId}) : super._();
  @override
  QueueStatusRequest rebuild(
          void Function(QueueStatusRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  QueueStatusRequestBuilder toBuilder() =>
      QueueStatusRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is QueueStatusRequest &&
        conversationId == other.conversationId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, conversationId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'QueueStatusRequest')
          ..add('conversationId', conversationId))
        .toString();
  }
}

class QueueStatusRequestBuilder
    implements Builder<QueueStatusRequest, QueueStatusRequestBuilder> {
  _$QueueStatusRequest? _$v;

  String? _conversationId;
  String? get conversationId => _$this._conversationId;
  set conversationId(String? conversationId) =>
      _$this._conversationId = conversationId;

  QueueStatusRequestBuilder() {
    QueueStatusRequest._defaults(this);
  }

  QueueStatusRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _conversationId = $v.conversationId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(QueueStatusRequest other) {
    _$v = other as _$QueueStatusRequest;
  }

  @override
  void update(void Function(QueueStatusRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  QueueStatusRequest build() => _build();

  _$QueueStatusRequest _build() {
    final _$result = _$v ??
        _$QueueStatusRequest._(
          conversationId: BuiltValueNullFieldError.checkNotNull(
              conversationId, r'QueueStatusRequest', 'conversationId'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
