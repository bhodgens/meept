// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'chat_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ChatResponse extends ChatResponse {
  @override
  final String reply;
  @override
  final String? model;
  @override
  final int? tokensUsed;

  factory _$ChatResponse([void Function(ChatResponseBuilder)? updates]) =>
      (ChatResponseBuilder()..update(updates))._build();

  _$ChatResponse._(
      {required this.reply,
      this.model,
      this.tokensUsed})
      : super._();
  @override
  ChatResponse rebuild(void Function(ChatResponseBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ChatResponseBuilder toBuilder() => ChatResponseBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ChatResponse &&
        reply == other.reply &&
        model == other.model &&
        tokensUsed == other.tokensUsed;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, reply.hashCode);
    _$hash = $jc(_$hash, model.hashCode);
    _$hash = $jc(_$hash, tokensUsed.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ChatResponse')
          ..add('reply', reply)
          ..add('model', model)
          ..add('tokensUsed', tokensUsed))
        .toString();
  }
}

class ChatResponseBuilder
    implements Builder<ChatResponse, ChatResponseBuilder> {
  _$ChatResponse? _$v;

  String? _reply;
  String? get reply => _$this._reply;
  set reply(String? reply) => _$this._reply = reply;

  String? _model;
  String? get model => _$this._model;
  set model(String? model) =>
      _$this._model = model;

  int? _tokensUsed;
  int? get tokensUsed => _$this._tokensUsed;
  set tokensUsed(int? tokensUsed) =>
      _$this._tokensUsed = tokensUsed;

  ChatResponseBuilder() {
    ChatResponse._defaults(this);
  }

  ChatResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _reply = $v.reply;
      _model = $v.model;
      _tokensUsed = $v.tokensUsed;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ChatResponse other) {
    _$v = other as _$ChatResponse;
  }

  @override
  void update(void Function(ChatResponseBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ChatResponse build() => _build();

  _$ChatResponse _build() {
    final _$result = _$v ??
        _$ChatResponse._(
          reply: BuiltValueNullFieldError.checkNotNull(
              reply, r'ChatResponse', 'reply'),
          model: model,
          tokensUsed: tokensUsed,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
