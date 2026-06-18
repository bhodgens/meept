// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'publish_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PublishRequest extends PublishRequest {
  @override
  final String topic;
  @override
  final String type;
  @override
  final String? sourceCommaOmitempty;
  @override
  final String? payloadCommaOmitempty;

  factory _$PublishRequest([void Function(PublishRequestBuilder)? updates]) =>
      (PublishRequestBuilder()..update(updates))._build();

  _$PublishRequest._(
      {required this.topic,
      required this.type,
      this.sourceCommaOmitempty,
      this.payloadCommaOmitempty})
      : super._();
  @override
  PublishRequest rebuild(void Function(PublishRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PublishRequestBuilder toBuilder() => PublishRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PublishRequest &&
        topic == other.topic &&
        type == other.type &&
        sourceCommaOmitempty == other.sourceCommaOmitempty &&
        payloadCommaOmitempty == other.payloadCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, topic.hashCode);
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, sourceCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, payloadCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PublishRequest')
          ..add('topic', topic)
          ..add('type', type)
          ..add('sourceCommaOmitempty', sourceCommaOmitempty)
          ..add('payloadCommaOmitempty', payloadCommaOmitempty))
        .toString();
  }
}

class PublishRequestBuilder
    implements Builder<PublishRequest, PublishRequestBuilder> {
  _$PublishRequest? _$v;

  String? _topic;
  String? get topic => _$this._topic;
  set topic(String? topic) => _$this._topic = topic;

  String? _type;
  String? get type => _$this._type;
  set type(String? type) => _$this._type = type;

  String? _sourceCommaOmitempty;
  String? get sourceCommaOmitempty => _$this._sourceCommaOmitempty;
  set sourceCommaOmitempty(String? sourceCommaOmitempty) =>
      _$this._sourceCommaOmitempty = sourceCommaOmitempty;

  String? _payloadCommaOmitempty;
  String? get payloadCommaOmitempty => _$this._payloadCommaOmitempty;
  set payloadCommaOmitempty(String? payloadCommaOmitempty) =>
      _$this._payloadCommaOmitempty = payloadCommaOmitempty;

  PublishRequestBuilder() {
    PublishRequest._defaults(this);
  }

  PublishRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _topic = $v.topic;
      _type = $v.type;
      _sourceCommaOmitempty = $v.sourceCommaOmitempty;
      _payloadCommaOmitempty = $v.payloadCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PublishRequest other) {
    _$v = other as _$PublishRequest;
  }

  @override
  void update(void Function(PublishRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PublishRequest build() => _build();

  _$PublishRequest _build() {
    final _$result = _$v ??
        _$PublishRequest._(
          topic: BuiltValueNullFieldError.checkNotNull(
              topic, r'PublishRequest', 'topic'),
          type: BuiltValueNullFieldError.checkNotNull(
              type, r'PublishRequest', 'type'),
          sourceCommaOmitempty: sourceCommaOmitempty,
          payloadCommaOmitempty: payloadCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
