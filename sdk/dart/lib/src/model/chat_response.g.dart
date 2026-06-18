// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'chat_response.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ChatResponse extends ChatResponse {
  @override
  final String reply;
  @override
  final String? modelCommaOmitempty;
  @override
  final int? tokensUsedCommaOmitempty;

  factory _$ChatResponse([void Function(ChatResponseBuilder)? updates]) =>
      (ChatResponseBuilder()..update(updates))._build();

  _$ChatResponse._(
      {required this.reply,
      this.modelCommaOmitempty,
      this.tokensUsedCommaOmitempty})
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
        modelCommaOmitempty == other.modelCommaOmitempty &&
        tokensUsedCommaOmitempty == other.tokensUsedCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, reply.hashCode);
    _$hash = $jc(_$hash, modelCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, tokensUsedCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ChatResponse')
          ..add('reply', reply)
          ..add('modelCommaOmitempty', modelCommaOmitempty)
          ..add('tokensUsedCommaOmitempty', tokensUsedCommaOmitempty))
        .toString();
  }
}

class ChatResponseBuilder
    implements Builder<ChatResponse, ChatResponseBuilder> {
  _$ChatResponse? _$v;

  String? _reply;
  String? get reply => _$this._reply;
  set reply(String? reply) => _$this._reply = reply;

  String? _modelCommaOmitempty;
  String? get modelCommaOmitempty => _$this._modelCommaOmitempty;
  set modelCommaOmitempty(String? modelCommaOmitempty) =>
      _$this._modelCommaOmitempty = modelCommaOmitempty;

  int? _tokensUsedCommaOmitempty;
  int? get tokensUsedCommaOmitempty => _$this._tokensUsedCommaOmitempty;
  set tokensUsedCommaOmitempty(int? tokensUsedCommaOmitempty) =>
      _$this._tokensUsedCommaOmitempty = tokensUsedCommaOmitempty;

  ChatResponseBuilder() {
    ChatResponse._defaults(this);
  }

  ChatResponseBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _reply = $v.reply;
      _modelCommaOmitempty = $v.modelCommaOmitempty;
      _tokensUsedCommaOmitempty = $v.tokensUsedCommaOmitempty;
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
          modelCommaOmitempty: modelCommaOmitempty,
          tokensUsedCommaOmitempty: tokensUsedCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
