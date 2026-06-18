// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'enqueue_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$EnqueueRequest extends EnqueueRequest {
  @override
  final String type;
  @override
  final int? priorityCommaOmitempty;
  @override
  final String? taskIdCommaOmitempty;
  @override
  final String prompt;
  @override
  final String? sessionIdCommaOmitempty;
  @override
  final String? requiredCapsCommaOmitempty;
  @override
  final String? payloadCommaOmitempty;

  factory _$EnqueueRequest([void Function(EnqueueRequestBuilder)? updates]) =>
      (EnqueueRequestBuilder()..update(updates))._build();

  _$EnqueueRequest._(
      {required this.type,
      this.priorityCommaOmitempty,
      this.taskIdCommaOmitempty,
      required this.prompt,
      this.sessionIdCommaOmitempty,
      this.requiredCapsCommaOmitempty,
      this.payloadCommaOmitempty})
      : super._();
  @override
  EnqueueRequest rebuild(void Function(EnqueueRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  EnqueueRequestBuilder toBuilder() => EnqueueRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is EnqueueRequest &&
        type == other.type &&
        priorityCommaOmitempty == other.priorityCommaOmitempty &&
        taskIdCommaOmitempty == other.taskIdCommaOmitempty &&
        prompt == other.prompt &&
        sessionIdCommaOmitempty == other.sessionIdCommaOmitempty &&
        requiredCapsCommaOmitempty == other.requiredCapsCommaOmitempty &&
        payloadCommaOmitempty == other.payloadCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, priorityCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, taskIdCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, prompt.hashCode);
    _$hash = $jc(_$hash, sessionIdCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, requiredCapsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, payloadCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'EnqueueRequest')
          ..add('type', type)
          ..add('priorityCommaOmitempty', priorityCommaOmitempty)
          ..add('taskIdCommaOmitempty', taskIdCommaOmitempty)
          ..add('prompt', prompt)
          ..add('sessionIdCommaOmitempty', sessionIdCommaOmitempty)
          ..add('requiredCapsCommaOmitempty', requiredCapsCommaOmitempty)
          ..add('payloadCommaOmitempty', payloadCommaOmitempty))
        .toString();
  }
}

class EnqueueRequestBuilder
    implements Builder<EnqueueRequest, EnqueueRequestBuilder> {
  _$EnqueueRequest? _$v;

  String? _type;
  String? get type => _$this._type;
  set type(String? type) => _$this._type = type;

  int? _priorityCommaOmitempty;
  int? get priorityCommaOmitempty => _$this._priorityCommaOmitempty;
  set priorityCommaOmitempty(int? priorityCommaOmitempty) =>
      _$this._priorityCommaOmitempty = priorityCommaOmitempty;

  String? _taskIdCommaOmitempty;
  String? get taskIdCommaOmitempty => _$this._taskIdCommaOmitempty;
  set taskIdCommaOmitempty(String? taskIdCommaOmitempty) =>
      _$this._taskIdCommaOmitempty = taskIdCommaOmitempty;

  String? _prompt;
  String? get prompt => _$this._prompt;
  set prompt(String? prompt) => _$this._prompt = prompt;

  String? _sessionIdCommaOmitempty;
  String? get sessionIdCommaOmitempty => _$this._sessionIdCommaOmitempty;
  set sessionIdCommaOmitempty(String? sessionIdCommaOmitempty) =>
      _$this._sessionIdCommaOmitempty = sessionIdCommaOmitempty;

  String? _requiredCapsCommaOmitempty;
  String? get requiredCapsCommaOmitempty => _$this._requiredCapsCommaOmitempty;
  set requiredCapsCommaOmitempty(String? requiredCapsCommaOmitempty) =>
      _$this._requiredCapsCommaOmitempty = requiredCapsCommaOmitempty;

  String? _payloadCommaOmitempty;
  String? get payloadCommaOmitempty => _$this._payloadCommaOmitempty;
  set payloadCommaOmitempty(String? payloadCommaOmitempty) =>
      _$this._payloadCommaOmitempty = payloadCommaOmitempty;

  EnqueueRequestBuilder() {
    EnqueueRequest._defaults(this);
  }

  EnqueueRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _type = $v.type;
      _priorityCommaOmitempty = $v.priorityCommaOmitempty;
      _taskIdCommaOmitempty = $v.taskIdCommaOmitempty;
      _prompt = $v.prompt;
      _sessionIdCommaOmitempty = $v.sessionIdCommaOmitempty;
      _requiredCapsCommaOmitempty = $v.requiredCapsCommaOmitempty;
      _payloadCommaOmitempty = $v.payloadCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(EnqueueRequest other) {
    _$v = other as _$EnqueueRequest;
  }

  @override
  void update(void Function(EnqueueRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  EnqueueRequest build() => _build();

  _$EnqueueRequest _build() {
    final _$result = _$v ??
        _$EnqueueRequest._(
          type: BuiltValueNullFieldError.checkNotNull(
              type, r'EnqueueRequest', 'type'),
          priorityCommaOmitempty: priorityCommaOmitempty,
          taskIdCommaOmitempty: taskIdCommaOmitempty,
          prompt: BuiltValueNullFieldError.checkNotNull(
              prompt, r'EnqueueRequest', 'prompt'),
          sessionIdCommaOmitempty: sessionIdCommaOmitempty,
          requiredCapsCommaOmitempty: requiredCapsCommaOmitempty,
          payloadCommaOmitempty: payloadCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
