// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'enqueue_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$EnqueueRequest extends EnqueueRequest {
  @override
  final String type;
  @override
  final int? priority;
  @override
  final String? taskId;
  @override
  final String prompt;
  @override
  final String? sessionId;
  @override
  final String? requiredCaps;
  @override
  final String? payload;

  factory _$EnqueueRequest([void Function(EnqueueRequestBuilder)? updates]) =>
      (EnqueueRequestBuilder()..update(updates))._build();

  _$EnqueueRequest._(
      {required this.type,
      this.priority,
      this.taskId,
      required this.prompt,
      this.sessionId,
      this.requiredCaps,
      this.payload})
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
        priority == other.priority &&
        taskId == other.taskId &&
        prompt == other.prompt &&
        sessionId == other.sessionId &&
        requiredCaps == other.requiredCaps &&
        payload == other.payload;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, priority.hashCode);
    _$hash = $jc(_$hash, taskId.hashCode);
    _$hash = $jc(_$hash, prompt.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jc(_$hash, requiredCaps.hashCode);
    _$hash = $jc(_$hash, payload.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'EnqueueRequest')
          ..add('type', type)
          ..add('priority', priority)
          ..add('taskId', taskId)
          ..add('prompt', prompt)
          ..add('sessionId', sessionId)
          ..add('requiredCaps', requiredCaps)
          ..add('payload', payload))
        .toString();
  }
}

class EnqueueRequestBuilder
    implements Builder<EnqueueRequest, EnqueueRequestBuilder> {
  _$EnqueueRequest? _$v;

  String? _type;
  String? get type => _$this._type;
  set type(String? type) => _$this._type = type;

  int? _priority;
  int? get priority => _$this._priority;
  set priority(int? priority) =>
      _$this._priority = priority;

  String? _taskId;
  String? get taskId => _$this._taskId;
  set taskId(String? taskId) =>
      _$this._taskId = taskId;

  String? _prompt;
  String? get prompt => _$this._prompt;
  set prompt(String? prompt) => _$this._prompt = prompt;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) =>
      _$this._sessionId = sessionId;

  String? _requiredCaps;
  String? get requiredCaps => _$this._requiredCaps;
  set requiredCaps(String? requiredCaps) =>
      _$this._requiredCaps = requiredCaps;

  String? _payload;
  String? get payload => _$this._payload;
  set payload(String? payload) =>
      _$this._payload = payload;

  EnqueueRequestBuilder() {
    EnqueueRequest._defaults(this);
  }

  EnqueueRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _type = $v.type;
      _priority = $v.priority;
      _taskId = $v.taskId;
      _prompt = $v.prompt;
      _sessionId = $v.sessionId;
      _requiredCaps = $v.requiredCaps;
      _payload = $v.payload;
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
          priority: priority,
          taskId: taskId,
          prompt: BuiltValueNullFieldError.checkNotNull(
              prompt, r'EnqueueRequest', 'prompt'),
          sessionId: sessionId,
          requiredCaps: requiredCaps,
          payload: payload,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
