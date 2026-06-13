// coverage:ignore-file
// GENERATED CODE - DO NOT MODIFY BY HAND
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'api_models.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

T _$identity<T>(T value) => value;

final _privateConstructorUsedError = UnsupportedError(
    'It seems like you constructed your class using `MyClass._()`. This constructor is only meant to be used by freezed and you are not supposed to need it nor use it.\nPlease check the documentation here for more information: https://github.com/rrousselGit/freezed#adding-getters-and-methods-to-our-models');

ChatMessage _$ChatMessageFromJson(Map<String, dynamic> json) {
  return _ChatMessage.fromJson(json);
}

/// @nodoc
mixin _$ChatMessage {
  String get id => throw _privateConstructorUsedError;
  String get role => throw _privateConstructorUsedError;
  String get content => throw _privateConstructorUsedError;
  DateTime get timestamp => throw _privateConstructorUsedError;
  @JsonKey(name: 'session_id')
  String? get sessionId => throw _privateConstructorUsedError;
  @JsonKey(name: 'tool_calls')
  List<String>? get toolCalls => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $ChatMessageCopyWith<ChatMessage> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $ChatMessageCopyWith<$Res> {
  factory $ChatMessageCopyWith(
          ChatMessage value, $Res Function(ChatMessage) then) =
      _$ChatMessageCopyWithImpl<$Res, ChatMessage>;
  @useResult
  $Res call(
      {String id,
      String role,
      String content,
      DateTime timestamp,
      @JsonKey(name: 'session_id') String? sessionId,
      @JsonKey(name: 'tool_calls') List<String>? toolCalls});
}

/// @nodoc
class _$ChatMessageCopyWithImpl<$Res, $Val extends ChatMessage>
    implements $ChatMessageCopyWith<$Res> {
  _$ChatMessageCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? role = null,
    Object? content = null,
    Object? timestamp = null,
    Object? sessionId = freezed,
    Object? toolCalls = freezed,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      role: null == role
          ? _value.role
          : role // ignore: cast_nullable_to_non_nullable
              as String,
      content: null == content
          ? _value.content
          : content // ignore: cast_nullable_to_non_nullable
              as String,
      timestamp: null == timestamp
          ? _value.timestamp
          : timestamp // ignore: cast_nullable_to_non_nullable
              as DateTime,
      sessionId: freezed == sessionId
          ? _value.sessionId
          : sessionId // ignore: cast_nullable_to_non_nullable
              as String?,
      toolCalls: freezed == toolCalls
          ? _value.toolCalls
          : toolCalls // ignore: cast_nullable_to_non_nullable
              as List<String>?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$ChatMessageImplCopyWith<$Res>
    implements $ChatMessageCopyWith<$Res> {
  factory _$$ChatMessageImplCopyWith(
          _$ChatMessageImpl value, $Res Function(_$ChatMessageImpl) then) =
      __$$ChatMessageImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      String role,
      String content,
      DateTime timestamp,
      @JsonKey(name: 'session_id') String? sessionId,
      @JsonKey(name: 'tool_calls') List<String>? toolCalls});
}

/// @nodoc
class __$$ChatMessageImplCopyWithImpl<$Res>
    extends _$ChatMessageCopyWithImpl<$Res, _$ChatMessageImpl>
    implements _$$ChatMessageImplCopyWith<$Res> {
  __$$ChatMessageImplCopyWithImpl(
      _$ChatMessageImpl _value, $Res Function(_$ChatMessageImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? role = null,
    Object? content = null,
    Object? timestamp = null,
    Object? sessionId = freezed,
    Object? toolCalls = freezed,
  }) {
    return _then(_$ChatMessageImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      role: null == role
          ? _value.role
          : role // ignore: cast_nullable_to_non_nullable
              as String,
      content: null == content
          ? _value.content
          : content // ignore: cast_nullable_to_non_nullable
              as String,
      timestamp: null == timestamp
          ? _value.timestamp
          : timestamp // ignore: cast_nullable_to_non_nullable
              as DateTime,
      sessionId: freezed == sessionId
          ? _value.sessionId
          : sessionId // ignore: cast_nullable_to_non_nullable
              as String?,
      toolCalls: freezed == toolCalls
          ? _value._toolCalls
          : toolCalls // ignore: cast_nullable_to_non_nullable
              as List<String>?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$ChatMessageImpl extends _ChatMessage {
  const _$ChatMessageImpl(
      {required this.id,
      required this.role,
      required this.content,
      required this.timestamp,
      @JsonKey(name: 'session_id') this.sessionId,
      @JsonKey(name: 'tool_calls') final List<String>? toolCalls})
      : _toolCalls = toolCalls,
        super._();

  factory _$ChatMessageImpl.fromJson(Map<String, dynamic> json) =>
      _$$ChatMessageImplFromJson(json);

  @override
  final String id;
  @override
  final String role;
  @override
  final String content;
  @override
  final DateTime timestamp;
  @override
  @JsonKey(name: 'session_id')
  final String? sessionId;
  final List<String>? _toolCalls;
  @override
  @JsonKey(name: 'tool_calls')
  List<String>? get toolCalls {
    final value = _toolCalls;
    if (value == null) return null;
    if (_toolCalls is EqualUnmodifiableListView) return _toolCalls;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(value);
  }

  @override
  String toString() {
    return 'ChatMessage(id: $id, role: $role, content: $content, timestamp: $timestamp, sessionId: $sessionId, toolCalls: $toolCalls)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$ChatMessageImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.role, role) || other.role == role) &&
            (identical(other.content, content) || other.content == content) &&
            (identical(other.timestamp, timestamp) ||
                other.timestamp == timestamp) &&
            (identical(other.sessionId, sessionId) ||
                other.sessionId == sessionId) &&
            const DeepCollectionEquality()
                .equals(other._toolCalls, _toolCalls));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, id, role, content, timestamp,
      sessionId, const DeepCollectionEquality().hash(_toolCalls));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$ChatMessageImplCopyWith<_$ChatMessageImpl> get copyWith =>
      __$$ChatMessageImplCopyWithImpl<_$ChatMessageImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$ChatMessageImplToJson(
      this,
    );
  }
}

abstract class _ChatMessage extends ChatMessage {
  const factory _ChatMessage(
          {required final String id,
          required final String role,
          required final String content,
          required final DateTime timestamp,
          @JsonKey(name: 'session_id') final String? sessionId,
          @JsonKey(name: 'tool_calls') final List<String>? toolCalls}) =
      _$ChatMessageImpl;
  const _ChatMessage._() : super._();

  factory _ChatMessage.fromJson(Map<String, dynamic> json) =
      _$ChatMessageImpl.fromJson;

  @override
  String get id;
  @override
  String get role;
  @override
  String get content;
  @override
  DateTime get timestamp;
  @override
  @JsonKey(name: 'session_id')
  String? get sessionId;
  @override
  @JsonKey(name: 'tool_calls')
  List<String>? get toolCalls;
  @override
  @JsonKey(ignore: true)
  _$$ChatMessageImplCopyWith<_$ChatMessageImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

Session _$SessionFromJson(Map<String, dynamic> json) {
  return _Session.fromJson(json);
}

/// @nodoc
mixin _$Session {
  String get id => throw _privateConstructorUsedError;

  /// Backend field name is 'name'; stored as 'title' in the Dart model.
  @JsonKey(name: 'name')
  String get title => throw _privateConstructorUsedError;
  String? get description => throw _privateConstructorUsedError;
  @JsonKey(name: 'conversation_id')
  String? get conversationId => throw _privateConstructorUsedError;
  @JsonKey(name: 'created_at')
  DateTime get createdAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'last_activity')
  DateTime? get lastActivity => throw _privateConstructorUsedError;
  @JsonKey(name: 'attached_clients')
  List<String>? get attachedClients => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $SessionCopyWith<Session> get copyWith => throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $SessionCopyWith<$Res> {
  factory $SessionCopyWith(Session value, $Res Function(Session) then) =
      _$SessionCopyWithImpl<$Res, Session>;
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'name') String title,
      String? description,
      @JsonKey(name: 'conversation_id') String? conversationId,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'last_activity') DateTime? lastActivity,
      @JsonKey(name: 'attached_clients') List<String>? attachedClients});
}

/// @nodoc
class _$SessionCopyWithImpl<$Res, $Val extends Session>
    implements $SessionCopyWith<$Res> {
  _$SessionCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? title = null,
    Object? description = freezed,
    Object? conversationId = freezed,
    Object? createdAt = null,
    Object? lastActivity = freezed,
    Object? attachedClients = freezed,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      description: freezed == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String?,
      conversationId: freezed == conversationId
          ? _value.conversationId
          : conversationId // ignore: cast_nullable_to_non_nullable
              as String?,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      lastActivity: freezed == lastActivity
          ? _value.lastActivity
          : lastActivity // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      attachedClients: freezed == attachedClients
          ? _value.attachedClients
          : attachedClients // ignore: cast_nullable_to_non_nullable
              as List<String>?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$SessionImplCopyWith<$Res> implements $SessionCopyWith<$Res> {
  factory _$$SessionImplCopyWith(
          _$SessionImpl value, $Res Function(_$SessionImpl) then) =
      __$$SessionImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'name') String title,
      String? description,
      @JsonKey(name: 'conversation_id') String? conversationId,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'last_activity') DateTime? lastActivity,
      @JsonKey(name: 'attached_clients') List<String>? attachedClients});
}

/// @nodoc
class __$$SessionImplCopyWithImpl<$Res>
    extends _$SessionCopyWithImpl<$Res, _$SessionImpl>
    implements _$$SessionImplCopyWith<$Res> {
  __$$SessionImplCopyWithImpl(
      _$SessionImpl _value, $Res Function(_$SessionImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? title = null,
    Object? description = freezed,
    Object? conversationId = freezed,
    Object? createdAt = null,
    Object? lastActivity = freezed,
    Object? attachedClients = freezed,
  }) {
    return _then(_$SessionImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      description: freezed == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String?,
      conversationId: freezed == conversationId
          ? _value.conversationId
          : conversationId // ignore: cast_nullable_to_non_nullable
              as String?,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      lastActivity: freezed == lastActivity
          ? _value.lastActivity
          : lastActivity // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      attachedClients: freezed == attachedClients
          ? _value._attachedClients
          : attachedClients // ignore: cast_nullable_to_non_nullable
              as List<String>?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$SessionImpl extends _Session {
  const _$SessionImpl(
      {required this.id,
      @JsonKey(name: 'name') required this.title,
      this.description,
      @JsonKey(name: 'conversation_id') this.conversationId,
      @JsonKey(name: 'created_at') required this.createdAt,
      @JsonKey(name: 'last_activity') this.lastActivity,
      @JsonKey(name: 'attached_clients') final List<String>? attachedClients})
      : _attachedClients = attachedClients,
        super._();

  factory _$SessionImpl.fromJson(Map<String, dynamic> json) =>
      _$$SessionImplFromJson(json);

  @override
  final String id;

  /// Backend field name is 'name'; stored as 'title' in the Dart model.
  @override
  @JsonKey(name: 'name')
  final String title;
  @override
  final String? description;
  @override
  @JsonKey(name: 'conversation_id')
  final String? conversationId;
  @override
  @JsonKey(name: 'created_at')
  final DateTime createdAt;
  @override
  @JsonKey(name: 'last_activity')
  final DateTime? lastActivity;
  final List<String>? _attachedClients;
  @override
  @JsonKey(name: 'attached_clients')
  List<String>? get attachedClients {
    final value = _attachedClients;
    if (value == null) return null;
    if (_attachedClients is EqualUnmodifiableListView) return _attachedClients;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(value);
  }

  @override
  String toString() {
    return 'Session(id: $id, title: $title, description: $description, conversationId: $conversationId, createdAt: $createdAt, lastActivity: $lastActivity, attachedClients: $attachedClients)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$SessionImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.title, title) || other.title == title) &&
            (identical(other.description, description) ||
                other.description == description) &&
            (identical(other.conversationId, conversationId) ||
                other.conversationId == conversationId) &&
            (identical(other.createdAt, createdAt) ||
                other.createdAt == createdAt) &&
            (identical(other.lastActivity, lastActivity) ||
                other.lastActivity == lastActivity) &&
            const DeepCollectionEquality()
                .equals(other._attachedClients, _attachedClients));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType,
      id,
      title,
      description,
      conversationId,
      createdAt,
      lastActivity,
      const DeepCollectionEquality().hash(_attachedClients));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$SessionImplCopyWith<_$SessionImpl> get copyWith =>
      __$$SessionImplCopyWithImpl<_$SessionImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$SessionImplToJson(
      this,
    );
  }
}

abstract class _Session extends Session {
  const factory _Session(
      {required final String id,
      @JsonKey(name: 'name') required final String title,
      final String? description,
      @JsonKey(name: 'conversation_id') final String? conversationId,
      @JsonKey(name: 'created_at') required final DateTime createdAt,
      @JsonKey(name: 'last_activity') final DateTime? lastActivity,
      @JsonKey(name: 'attached_clients')
      final List<String>? attachedClients}) = _$SessionImpl;
  const _Session._() : super._();

  factory _Session.fromJson(Map<String, dynamic> json) = _$SessionImpl.fromJson;

  @override
  String get id;
  @override

  /// Backend field name is 'name'; stored as 'title' in the Dart model.
  @JsonKey(name: 'name')
  String get title;
  @override
  String? get description;
  @override
  @JsonKey(name: 'conversation_id')
  String? get conversationId;
  @override
  @JsonKey(name: 'created_at')
  DateTime get createdAt;
  @override
  @JsonKey(name: 'last_activity')
  DateTime? get lastActivity;
  @override
  @JsonKey(name: 'attached_clients')
  List<String>? get attachedClients;
  @override
  @JsonKey(ignore: true)
  _$$SessionImplCopyWith<_$SessionImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

Task _$TaskFromJson(Map<String, dynamic> json) {
  return _Task.fromJson(json);
}

/// @nodoc
mixin _$Task {
  String get id => throw _privateConstructorUsedError;

  /// Backend field name is 'name'; stored as 'title' in the Dart model.
  @JsonKey(name: 'name')
  String get title => throw _privateConstructorUsedError;
  String get description => throw _privateConstructorUsedError;

  /// Backend field name is 'state'; stored as 'status' in the Dart model.
  @JsonKey(name: 'state')
  String get status => throw _privateConstructorUsedError;
  @JsonKey(name: 'agent_id')
  String? get agentId => throw _privateConstructorUsedError;
  @JsonKey(name: 'session_id')
  String? get sessionId => throw _privateConstructorUsedError;
  @JsonKey(name: 'created_at')
  DateTime get createdAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'updated_at')
  DateTime? get updatedAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'completed_at')
  DateTime? get completedAt => throw _privateConstructorUsedError;
  Map<String, dynamic>? get metadata => throw _privateConstructorUsedError;
  @JsonKey(name: 'total_jobs')
  int? get totalJobs => throw _privateConstructorUsedError;
  @JsonKey(name: 'completed_jobs')
  int? get completedJobs => throw _privateConstructorUsedError;
  @JsonKey(name: 'failed_jobs')
  int? get failedJobs => throw _privateConstructorUsedError;
  List<TaskStep>? get steps => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $TaskCopyWith<Task> get copyWith => throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $TaskCopyWith<$Res> {
  factory $TaskCopyWith(Task value, $Res Function(Task) then) =
      _$TaskCopyWithImpl<$Res, Task>;
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'name') String title,
      String description,
      @JsonKey(name: 'state') String status,
      @JsonKey(name: 'agent_id') String? agentId,
      @JsonKey(name: 'session_id') String? sessionId,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'updated_at') DateTime? updatedAt,
      @JsonKey(name: 'completed_at') DateTime? completedAt,
      Map<String, dynamic>? metadata,
      @JsonKey(name: 'total_jobs') int? totalJobs,
      @JsonKey(name: 'completed_jobs') int? completedJobs,
      @JsonKey(name: 'failed_jobs') int? failedJobs,
      List<TaskStep>? steps});
}

/// @nodoc
class _$TaskCopyWithImpl<$Res, $Val extends Task>
    implements $TaskCopyWith<$Res> {
  _$TaskCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? title = null,
    Object? description = null,
    Object? status = null,
    Object? agentId = freezed,
    Object? sessionId = freezed,
    Object? createdAt = null,
    Object? updatedAt = freezed,
    Object? completedAt = freezed,
    Object? metadata = freezed,
    Object? totalJobs = freezed,
    Object? completedJobs = freezed,
    Object? failedJobs = freezed,
    Object? steps = freezed,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      status: null == status
          ? _value.status
          : status // ignore: cast_nullable_to_non_nullable
              as String,
      agentId: freezed == agentId
          ? _value.agentId
          : agentId // ignore: cast_nullable_to_non_nullable
              as String?,
      sessionId: freezed == sessionId
          ? _value.sessionId
          : sessionId // ignore: cast_nullable_to_non_nullable
              as String?,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      updatedAt: freezed == updatedAt
          ? _value.updatedAt
          : updatedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      completedAt: freezed == completedAt
          ? _value.completedAt
          : completedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      metadata: freezed == metadata
          ? _value.metadata
          : metadata // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
      totalJobs: freezed == totalJobs
          ? _value.totalJobs
          : totalJobs // ignore: cast_nullable_to_non_nullable
              as int?,
      completedJobs: freezed == completedJobs
          ? _value.completedJobs
          : completedJobs // ignore: cast_nullable_to_non_nullable
              as int?,
      failedJobs: freezed == failedJobs
          ? _value.failedJobs
          : failedJobs // ignore: cast_nullable_to_non_nullable
              as int?,
      steps: freezed == steps
          ? _value.steps
          : steps // ignore: cast_nullable_to_non_nullable
              as List<TaskStep>?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$TaskImplCopyWith<$Res> implements $TaskCopyWith<$Res> {
  factory _$$TaskImplCopyWith(
          _$TaskImpl value, $Res Function(_$TaskImpl) then) =
      __$$TaskImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'name') String title,
      String description,
      @JsonKey(name: 'state') String status,
      @JsonKey(name: 'agent_id') String? agentId,
      @JsonKey(name: 'session_id') String? sessionId,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'updated_at') DateTime? updatedAt,
      @JsonKey(name: 'completed_at') DateTime? completedAt,
      Map<String, dynamic>? metadata,
      @JsonKey(name: 'total_jobs') int? totalJobs,
      @JsonKey(name: 'completed_jobs') int? completedJobs,
      @JsonKey(name: 'failed_jobs') int? failedJobs,
      List<TaskStep>? steps});
}

/// @nodoc
class __$$TaskImplCopyWithImpl<$Res>
    extends _$TaskCopyWithImpl<$Res, _$TaskImpl>
    implements _$$TaskImplCopyWith<$Res> {
  __$$TaskImplCopyWithImpl(_$TaskImpl _value, $Res Function(_$TaskImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? title = null,
    Object? description = null,
    Object? status = null,
    Object? agentId = freezed,
    Object? sessionId = freezed,
    Object? createdAt = null,
    Object? updatedAt = freezed,
    Object? completedAt = freezed,
    Object? metadata = freezed,
    Object? totalJobs = freezed,
    Object? completedJobs = freezed,
    Object? failedJobs = freezed,
    Object? steps = freezed,
  }) {
    return _then(_$TaskImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      status: null == status
          ? _value.status
          : status // ignore: cast_nullable_to_non_nullable
              as String,
      agentId: freezed == agentId
          ? _value.agentId
          : agentId // ignore: cast_nullable_to_non_nullable
              as String?,
      sessionId: freezed == sessionId
          ? _value.sessionId
          : sessionId // ignore: cast_nullable_to_non_nullable
              as String?,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      updatedAt: freezed == updatedAt
          ? _value.updatedAt
          : updatedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      completedAt: freezed == completedAt
          ? _value.completedAt
          : completedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      metadata: freezed == metadata
          ? _value._metadata
          : metadata // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
      totalJobs: freezed == totalJobs
          ? _value.totalJobs
          : totalJobs // ignore: cast_nullable_to_non_nullable
              as int?,
      completedJobs: freezed == completedJobs
          ? _value.completedJobs
          : completedJobs // ignore: cast_nullable_to_non_nullable
              as int?,
      failedJobs: freezed == failedJobs
          ? _value.failedJobs
          : failedJobs // ignore: cast_nullable_to_non_nullable
              as int?,
      steps: freezed == steps
          ? _value._steps
          : steps // ignore: cast_nullable_to_non_nullable
              as List<TaskStep>?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$TaskImpl extends _Task {
  const _$TaskImpl(
      {required this.id,
      @JsonKey(name: 'name') required this.title,
      required this.description,
      @JsonKey(name: 'state') required this.status,
      @JsonKey(name: 'agent_id') this.agentId,
      @JsonKey(name: 'session_id') this.sessionId,
      @JsonKey(name: 'created_at') required this.createdAt,
      @JsonKey(name: 'updated_at') this.updatedAt,
      @JsonKey(name: 'completed_at') this.completedAt,
      final Map<String, dynamic>? metadata,
      @JsonKey(name: 'total_jobs') this.totalJobs,
      @JsonKey(name: 'completed_jobs') this.completedJobs,
      @JsonKey(name: 'failed_jobs') this.failedJobs,
      final List<TaskStep>? steps})
      : _metadata = metadata,
        _steps = steps,
        super._();

  factory _$TaskImpl.fromJson(Map<String, dynamic> json) =>
      _$$TaskImplFromJson(json);

  @override
  final String id;

  /// Backend field name is 'name'; stored as 'title' in the Dart model.
  @override
  @JsonKey(name: 'name')
  final String title;
  @override
  final String description;

  /// Backend field name is 'state'; stored as 'status' in the Dart model.
  @override
  @JsonKey(name: 'state')
  final String status;
  @override
  @JsonKey(name: 'agent_id')
  final String? agentId;
  @override
  @JsonKey(name: 'session_id')
  final String? sessionId;
  @override
  @JsonKey(name: 'created_at')
  final DateTime createdAt;
  @override
  @JsonKey(name: 'updated_at')
  final DateTime? updatedAt;
  @override
  @JsonKey(name: 'completed_at')
  final DateTime? completedAt;
  final Map<String, dynamic>? _metadata;
  @override
  Map<String, dynamic>? get metadata {
    final value = _metadata;
    if (value == null) return null;
    if (_metadata is EqualUnmodifiableMapView) return _metadata;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableMapView(value);
  }

  @override
  @JsonKey(name: 'total_jobs')
  final int? totalJobs;
  @override
  @JsonKey(name: 'completed_jobs')
  final int? completedJobs;
  @override
  @JsonKey(name: 'failed_jobs')
  final int? failedJobs;
  final List<TaskStep>? _steps;
  @override
  List<TaskStep>? get steps {
    final value = _steps;
    if (value == null) return null;
    if (_steps is EqualUnmodifiableListView) return _steps;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(value);
  }

  @override
  String toString() {
    return 'Task(id: $id, title: $title, description: $description, status: $status, agentId: $agentId, sessionId: $sessionId, createdAt: $createdAt, updatedAt: $updatedAt, completedAt: $completedAt, metadata: $metadata, totalJobs: $totalJobs, completedJobs: $completedJobs, failedJobs: $failedJobs, steps: $steps)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$TaskImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.title, title) || other.title == title) &&
            (identical(other.description, description) ||
                other.description == description) &&
            (identical(other.status, status) || other.status == status) &&
            (identical(other.agentId, agentId) || other.agentId == agentId) &&
            (identical(other.sessionId, sessionId) ||
                other.sessionId == sessionId) &&
            (identical(other.createdAt, createdAt) ||
                other.createdAt == createdAt) &&
            (identical(other.updatedAt, updatedAt) ||
                other.updatedAt == updatedAt) &&
            (identical(other.completedAt, completedAt) ||
                other.completedAt == completedAt) &&
            const DeepCollectionEquality().equals(other._metadata, _metadata) &&
            (identical(other.totalJobs, totalJobs) ||
                other.totalJobs == totalJobs) &&
            (identical(other.completedJobs, completedJobs) ||
                other.completedJobs == completedJobs) &&
            (identical(other.failedJobs, failedJobs) ||
                other.failedJobs == failedJobs) &&
            const DeepCollectionEquality().equals(other._steps, _steps));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType,
      id,
      title,
      description,
      status,
      agentId,
      sessionId,
      createdAt,
      updatedAt,
      completedAt,
      const DeepCollectionEquality().hash(_metadata),
      totalJobs,
      completedJobs,
      failedJobs,
      const DeepCollectionEquality().hash(_steps));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$TaskImplCopyWith<_$TaskImpl> get copyWith =>
      __$$TaskImplCopyWithImpl<_$TaskImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$TaskImplToJson(
      this,
    );
  }
}

abstract class _Task extends Task {
  const factory _Task(
      {required final String id,
      @JsonKey(name: 'name') required final String title,
      required final String description,
      @JsonKey(name: 'state') required final String status,
      @JsonKey(name: 'agent_id') final String? agentId,
      @JsonKey(name: 'session_id') final String? sessionId,
      @JsonKey(name: 'created_at') required final DateTime createdAt,
      @JsonKey(name: 'updated_at') final DateTime? updatedAt,
      @JsonKey(name: 'completed_at') final DateTime? completedAt,
      final Map<String, dynamic>? metadata,
      @JsonKey(name: 'total_jobs') final int? totalJobs,
      @JsonKey(name: 'completed_jobs') final int? completedJobs,
      @JsonKey(name: 'failed_jobs') final int? failedJobs,
      final List<TaskStep>? steps}) = _$TaskImpl;
  const _Task._() : super._();

  factory _Task.fromJson(Map<String, dynamic> json) = _$TaskImpl.fromJson;

  @override
  String get id;
  @override

  /// Backend field name is 'name'; stored as 'title' in the Dart model.
  @JsonKey(name: 'name')
  String get title;
  @override
  String get description;
  @override

  /// Backend field name is 'state'; stored as 'status' in the Dart model.
  @JsonKey(name: 'state')
  String get status;
  @override
  @JsonKey(name: 'agent_id')
  String? get agentId;
  @override
  @JsonKey(name: 'session_id')
  String? get sessionId;
  @override
  @JsonKey(name: 'created_at')
  DateTime get createdAt;
  @override
  @JsonKey(name: 'updated_at')
  DateTime? get updatedAt;
  @override
  @JsonKey(name: 'completed_at')
  DateTime? get completedAt;
  @override
  Map<String, dynamic>? get metadata;
  @override
  @JsonKey(name: 'total_jobs')
  int? get totalJobs;
  @override
  @JsonKey(name: 'completed_jobs')
  int? get completedJobs;
  @override
  @JsonKey(name: 'failed_jobs')
  int? get failedJobs;
  @override
  List<TaskStep>? get steps;
  @override
  @JsonKey(ignore: true)
  _$$TaskImplCopyWith<_$TaskImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

TaskStep _$TaskStepFromJson(Map<String, dynamic> json) {
  return _TaskStep.fromJson(json);
}

/// @nodoc
mixin _$TaskStep {
  String get id => throw _privateConstructorUsedError;
  @JsonKey(name: 'task_id')
  String get taskId => throw _privateConstructorUsedError;
  String get description => throw _privateConstructorUsedError;
  String get status => throw _privateConstructorUsedError;
  String? get output => throw _privateConstructorUsedError;
  @JsonKey(name: 'completed_at')
  DateTime? get completedAt => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $TaskStepCopyWith<TaskStep> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $TaskStepCopyWith<$Res> {
  factory $TaskStepCopyWith(TaskStep value, $Res Function(TaskStep) then) =
      _$TaskStepCopyWithImpl<$Res, TaskStep>;
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'task_id') String taskId,
      String description,
      String status,
      String? output,
      @JsonKey(name: 'completed_at') DateTime? completedAt});
}

/// @nodoc
class _$TaskStepCopyWithImpl<$Res, $Val extends TaskStep>
    implements $TaskStepCopyWith<$Res> {
  _$TaskStepCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? taskId = null,
    Object? description = null,
    Object? status = null,
    Object? output = freezed,
    Object? completedAt = freezed,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      taskId: null == taskId
          ? _value.taskId
          : taskId // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      status: null == status
          ? _value.status
          : status // ignore: cast_nullable_to_non_nullable
              as String,
      output: freezed == output
          ? _value.output
          : output // ignore: cast_nullable_to_non_nullable
              as String?,
      completedAt: freezed == completedAt
          ? _value.completedAt
          : completedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$TaskStepImplCopyWith<$Res>
    implements $TaskStepCopyWith<$Res> {
  factory _$$TaskStepImplCopyWith(
          _$TaskStepImpl value, $Res Function(_$TaskStepImpl) then) =
      __$$TaskStepImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'task_id') String taskId,
      String description,
      String status,
      String? output,
      @JsonKey(name: 'completed_at') DateTime? completedAt});
}

/// @nodoc
class __$$TaskStepImplCopyWithImpl<$Res>
    extends _$TaskStepCopyWithImpl<$Res, _$TaskStepImpl>
    implements _$$TaskStepImplCopyWith<$Res> {
  __$$TaskStepImplCopyWithImpl(
      _$TaskStepImpl _value, $Res Function(_$TaskStepImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? taskId = null,
    Object? description = null,
    Object? status = null,
    Object? output = freezed,
    Object? completedAt = freezed,
  }) {
    return _then(_$TaskStepImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      taskId: null == taskId
          ? _value.taskId
          : taskId // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      status: null == status
          ? _value.status
          : status // ignore: cast_nullable_to_non_nullable
              as String,
      output: freezed == output
          ? _value.output
          : output // ignore: cast_nullable_to_non_nullable
              as String?,
      completedAt: freezed == completedAt
          ? _value.completedAt
          : completedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$TaskStepImpl implements _TaskStep {
  const _$TaskStepImpl(
      {required this.id,
      @JsonKey(name: 'task_id') required this.taskId,
      required this.description,
      required this.status,
      this.output,
      @JsonKey(name: 'completed_at') this.completedAt});

  factory _$TaskStepImpl.fromJson(Map<String, dynamic> json) =>
      _$$TaskStepImplFromJson(json);

  @override
  final String id;
  @override
  @JsonKey(name: 'task_id')
  final String taskId;
  @override
  final String description;
  @override
  final String status;
  @override
  final String? output;
  @override
  @JsonKey(name: 'completed_at')
  final DateTime? completedAt;

  @override
  String toString() {
    return 'TaskStep(id: $id, taskId: $taskId, description: $description, status: $status, output: $output, completedAt: $completedAt)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$TaskStepImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.taskId, taskId) || other.taskId == taskId) &&
            (identical(other.description, description) ||
                other.description == description) &&
            (identical(other.status, status) || other.status == status) &&
            (identical(other.output, output) || other.output == output) &&
            (identical(other.completedAt, completedAt) ||
                other.completedAt == completedAt));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType, id, taskId, description, status, output, completedAt);

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$TaskStepImplCopyWith<_$TaskStepImpl> get copyWith =>
      __$$TaskStepImplCopyWithImpl<_$TaskStepImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$TaskStepImplToJson(
      this,
    );
  }
}

abstract class _TaskStep implements TaskStep {
  const factory _TaskStep(
          {required final String id,
          @JsonKey(name: 'task_id') required final String taskId,
          required final String description,
          required final String status,
          final String? output,
          @JsonKey(name: 'completed_at') final DateTime? completedAt}) =
      _$TaskStepImpl;

  factory _TaskStep.fromJson(Map<String, dynamic> json) =
      _$TaskStepImpl.fromJson;

  @override
  String get id;
  @override
  @JsonKey(name: 'task_id')
  String get taskId;
  @override
  String get description;
  @override
  String get status;
  @override
  String? get output;
  @override
  @JsonKey(name: 'completed_at')
  DateTime? get completedAt;
  @override
  @JsonKey(ignore: true)
  _$$TaskStepImplCopyWith<_$TaskStepImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

Agent _$AgentFromJson(Map<String, dynamic> json) {
  return _Agent.fromJson(json);
}

/// @nodoc
mixin _$Agent {
  String get id => throw _privateConstructorUsedError;
  String get name => throw _privateConstructorUsedError;
  String get description => throw _privateConstructorUsedError;
  bool get enabled => throw _privateConstructorUsedError;
  String? get prompt => throw _privateConstructorUsedError;
  Map<String, dynamic>? get frontmatter => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $AgentCopyWith<Agent> get copyWith => throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $AgentCopyWith<$Res> {
  factory $AgentCopyWith(Agent value, $Res Function(Agent) then) =
      _$AgentCopyWithImpl<$Res, Agent>;
  @useResult
  $Res call(
      {String id,
      String name,
      String description,
      bool enabled,
      String? prompt,
      Map<String, dynamic>? frontmatter});
}

/// @nodoc
class _$AgentCopyWithImpl<$Res, $Val extends Agent>
    implements $AgentCopyWith<$Res> {
  _$AgentCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? name = null,
    Object? description = null,
    Object? enabled = null,
    Object? prompt = freezed,
    Object? frontmatter = freezed,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      enabled: null == enabled
          ? _value.enabled
          : enabled // ignore: cast_nullable_to_non_nullable
              as bool,
      prompt: freezed == prompt
          ? _value.prompt
          : prompt // ignore: cast_nullable_to_non_nullable
              as String?,
      frontmatter: freezed == frontmatter
          ? _value.frontmatter
          : frontmatter // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$AgentImplCopyWith<$Res> implements $AgentCopyWith<$Res> {
  factory _$$AgentImplCopyWith(
          _$AgentImpl value, $Res Function(_$AgentImpl) then) =
      __$$AgentImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      String name,
      String description,
      bool enabled,
      String? prompt,
      Map<String, dynamic>? frontmatter});
}

/// @nodoc
class __$$AgentImplCopyWithImpl<$Res>
    extends _$AgentCopyWithImpl<$Res, _$AgentImpl>
    implements _$$AgentImplCopyWith<$Res> {
  __$$AgentImplCopyWithImpl(
      _$AgentImpl _value, $Res Function(_$AgentImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? name = null,
    Object? description = null,
    Object? enabled = null,
    Object? prompt = freezed,
    Object? frontmatter = freezed,
  }) {
    return _then(_$AgentImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      enabled: null == enabled
          ? _value.enabled
          : enabled // ignore: cast_nullable_to_non_nullable
              as bool,
      prompt: freezed == prompt
          ? _value.prompt
          : prompt // ignore: cast_nullable_to_non_nullable
              as String?,
      frontmatter: freezed == frontmatter
          ? _value._frontmatter
          : frontmatter // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$AgentImpl implements _Agent {
  const _$AgentImpl(
      {required this.id,
      required this.name,
      this.description = '',
      this.enabled = true,
      this.prompt,
      final Map<String, dynamic>? frontmatter})
      : _frontmatter = frontmatter;

  factory _$AgentImpl.fromJson(Map<String, dynamic> json) =>
      _$$AgentImplFromJson(json);

  @override
  final String id;
  @override
  final String name;
  @override
  @JsonKey()
  final String description;
  @override
  @JsonKey()
  final bool enabled;
  @override
  final String? prompt;
  final Map<String, dynamic>? _frontmatter;
  @override
  Map<String, dynamic>? get frontmatter {
    final value = _frontmatter;
    if (value == null) return null;
    if (_frontmatter is EqualUnmodifiableMapView) return _frontmatter;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableMapView(value);
  }

  @override
  String toString() {
    return 'Agent(id: $id, name: $name, description: $description, enabled: $enabled, prompt: $prompt, frontmatter: $frontmatter)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$AgentImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.name, name) || other.name == name) &&
            (identical(other.description, description) ||
                other.description == description) &&
            (identical(other.enabled, enabled) || other.enabled == enabled) &&
            (identical(other.prompt, prompt) || other.prompt == prompt) &&
            const DeepCollectionEquality()
                .equals(other._frontmatter, _frontmatter));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, id, name, description, enabled,
      prompt, const DeepCollectionEquality().hash(_frontmatter));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$AgentImplCopyWith<_$AgentImpl> get copyWith =>
      __$$AgentImplCopyWithImpl<_$AgentImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$AgentImplToJson(
      this,
    );
  }
}

abstract class _Agent implements Agent {
  const factory _Agent(
      {required final String id,
      required final String name,
      final String description,
      final bool enabled,
      final String? prompt,
      final Map<String, dynamic>? frontmatter}) = _$AgentImpl;

  factory _Agent.fromJson(Map<String, dynamic> json) = _$AgentImpl.fromJson;

  @override
  String get id;
  @override
  String get name;
  @override
  String get description;
  @override
  bool get enabled;
  @override
  String? get prompt;
  @override
  Map<String, dynamic>? get frontmatter;
  @override
  @JsonKey(ignore: true)
  _$$AgentImplCopyWith<_$AgentImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

Job _$JobFromJson(Map<String, dynamic> json) {
  return _Job.fromJson(json);
}

/// @nodoc
mixin _$Job {
  String get id => throw _privateConstructorUsedError;
  String get type => throw _privateConstructorUsedError;

  /// Backend field name is 'state'; stored as 'status' in the Dart model.
  @JsonKey(name: 'state')
  String get status => throw _privateConstructorUsedError;
  @JsonKey(name: 'agent_id')
  String? get agentId => throw _privateConstructorUsedError;
  Map<String, dynamic>? get payload => throw _privateConstructorUsedError;
  @JsonKey(name: 'created_at')
  DateTime get createdAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'completed_at')
  DateTime? get completedAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'retry_count')
  int get retryCount => throw _privateConstructorUsedError;
  String? get error => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $JobCopyWith<Job> get copyWith => throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $JobCopyWith<$Res> {
  factory $JobCopyWith(Job value, $Res Function(Job) then) =
      _$JobCopyWithImpl<$Res, Job>;
  @useResult
  $Res call(
      {String id,
      String type,
      @JsonKey(name: 'state') String status,
      @JsonKey(name: 'agent_id') String? agentId,
      Map<String, dynamic>? payload,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'completed_at') DateTime? completedAt,
      @JsonKey(name: 'retry_count') int retryCount,
      String? error});
}

/// @nodoc
class _$JobCopyWithImpl<$Res, $Val extends Job> implements $JobCopyWith<$Res> {
  _$JobCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? type = null,
    Object? status = null,
    Object? agentId = freezed,
    Object? payload = freezed,
    Object? createdAt = null,
    Object? completedAt = freezed,
    Object? retryCount = null,
    Object? error = freezed,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      type: null == type
          ? _value.type
          : type // ignore: cast_nullable_to_non_nullable
              as String,
      status: null == status
          ? _value.status
          : status // ignore: cast_nullable_to_non_nullable
              as String,
      agentId: freezed == agentId
          ? _value.agentId
          : agentId // ignore: cast_nullable_to_non_nullable
              as String?,
      payload: freezed == payload
          ? _value.payload
          : payload // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      completedAt: freezed == completedAt
          ? _value.completedAt
          : completedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      retryCount: null == retryCount
          ? _value.retryCount
          : retryCount // ignore: cast_nullable_to_non_nullable
              as int,
      error: freezed == error
          ? _value.error
          : error // ignore: cast_nullable_to_non_nullable
              as String?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$JobImplCopyWith<$Res> implements $JobCopyWith<$Res> {
  factory _$$JobImplCopyWith(_$JobImpl value, $Res Function(_$JobImpl) then) =
      __$$JobImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      String type,
      @JsonKey(name: 'state') String status,
      @JsonKey(name: 'agent_id') String? agentId,
      Map<String, dynamic>? payload,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'completed_at') DateTime? completedAt,
      @JsonKey(name: 'retry_count') int retryCount,
      String? error});
}

/// @nodoc
class __$$JobImplCopyWithImpl<$Res> extends _$JobCopyWithImpl<$Res, _$JobImpl>
    implements _$$JobImplCopyWith<$Res> {
  __$$JobImplCopyWithImpl(_$JobImpl _value, $Res Function(_$JobImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? type = null,
    Object? status = null,
    Object? agentId = freezed,
    Object? payload = freezed,
    Object? createdAt = null,
    Object? completedAt = freezed,
    Object? retryCount = null,
    Object? error = freezed,
  }) {
    return _then(_$JobImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      type: null == type
          ? _value.type
          : type // ignore: cast_nullable_to_non_nullable
              as String,
      status: null == status
          ? _value.status
          : status // ignore: cast_nullable_to_non_nullable
              as String,
      agentId: freezed == agentId
          ? _value.agentId
          : agentId // ignore: cast_nullable_to_non_nullable
              as String?,
      payload: freezed == payload
          ? _value._payload
          : payload // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      completedAt: freezed == completedAt
          ? _value.completedAt
          : completedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      retryCount: null == retryCount
          ? _value.retryCount
          : retryCount // ignore: cast_nullable_to_non_nullable
              as int,
      error: freezed == error
          ? _value.error
          : error // ignore: cast_nullable_to_non_nullable
              as String?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$JobImpl implements _Job {
  const _$JobImpl(
      {required this.id,
      required this.type,
      @JsonKey(name: 'state') required this.status,
      @JsonKey(name: 'agent_id') this.agentId,
      final Map<String, dynamic>? payload,
      @JsonKey(name: 'created_at') required this.createdAt,
      @JsonKey(name: 'completed_at') this.completedAt,
      @JsonKey(name: 'retry_count') this.retryCount = 0,
      this.error})
      : _payload = payload;

  factory _$JobImpl.fromJson(Map<String, dynamic> json) =>
      _$$JobImplFromJson(json);

  @override
  final String id;
  @override
  final String type;

  /// Backend field name is 'state'; stored as 'status' in the Dart model.
  @override
  @JsonKey(name: 'state')
  final String status;
  @override
  @JsonKey(name: 'agent_id')
  final String? agentId;
  final Map<String, dynamic>? _payload;
  @override
  Map<String, dynamic>? get payload {
    final value = _payload;
    if (value == null) return null;
    if (_payload is EqualUnmodifiableMapView) return _payload;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableMapView(value);
  }

  @override
  @JsonKey(name: 'created_at')
  final DateTime createdAt;
  @override
  @JsonKey(name: 'completed_at')
  final DateTime? completedAt;
  @override
  @JsonKey(name: 'retry_count')
  final int retryCount;
  @override
  final String? error;

  @override
  String toString() {
    return 'Job(id: $id, type: $type, status: $status, agentId: $agentId, payload: $payload, createdAt: $createdAt, completedAt: $completedAt, retryCount: $retryCount, error: $error)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$JobImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.type, type) || other.type == type) &&
            (identical(other.status, status) || other.status == status) &&
            (identical(other.agentId, agentId) || other.agentId == agentId) &&
            const DeepCollectionEquality().equals(other._payload, _payload) &&
            (identical(other.createdAt, createdAt) ||
                other.createdAt == createdAt) &&
            (identical(other.completedAt, completedAt) ||
                other.completedAt == completedAt) &&
            (identical(other.retryCount, retryCount) ||
                other.retryCount == retryCount) &&
            (identical(other.error, error) || other.error == error));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType,
      id,
      type,
      status,
      agentId,
      const DeepCollectionEquality().hash(_payload),
      createdAt,
      completedAt,
      retryCount,
      error);

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$JobImplCopyWith<_$JobImpl> get copyWith =>
      __$$JobImplCopyWithImpl<_$JobImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$JobImplToJson(
      this,
    );
  }
}

abstract class _Job implements Job {
  const factory _Job(
      {required final String id,
      required final String type,
      @JsonKey(name: 'state') required final String status,
      @JsonKey(name: 'agent_id') final String? agentId,
      final Map<String, dynamic>? payload,
      @JsonKey(name: 'created_at') required final DateTime createdAt,
      @JsonKey(name: 'completed_at') final DateTime? completedAt,
      @JsonKey(name: 'retry_count') final int retryCount,
      final String? error}) = _$JobImpl;

  factory _Job.fromJson(Map<String, dynamic> json) = _$JobImpl.fromJson;

  @override
  String get id;
  @override
  String get type;
  @override

  /// Backend field name is 'state'; stored as 'status' in the Dart model.
  @JsonKey(name: 'state')
  String get status;
  @override
  @JsonKey(name: 'agent_id')
  String? get agentId;
  @override
  Map<String, dynamic>? get payload;
  @override
  @JsonKey(name: 'created_at')
  DateTime get createdAt;
  @override
  @JsonKey(name: 'completed_at')
  DateTime? get completedAt;
  @override
  @JsonKey(name: 'retry_count')
  int get retryCount;
  @override
  String? get error;
  @override
  @JsonKey(ignore: true)
  _$$JobImplCopyWith<_$JobImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

Skill _$SkillFromJson(Map<String, dynamic> json) {
  return _Skill.fromJson(json);
}

/// @nodoc
mixin _$Skill {
  String get slug => throw _privateConstructorUsedError;
  String get name => throw _privateConstructorUsedError;
  String get description => throw _privateConstructorUsedError;
  String get category => throw _privateConstructorUsedError;
  List<String> get capabilities => throw _privateConstructorUsedError;
  List<String> get tags => throw _privateConstructorUsedError;
  bool get enabled => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $SkillCopyWith<Skill> get copyWith => throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $SkillCopyWith<$Res> {
  factory $SkillCopyWith(Skill value, $Res Function(Skill) then) =
      _$SkillCopyWithImpl<$Res, Skill>;
  @useResult
  $Res call(
      {String slug,
      String name,
      String description,
      String category,
      List<String> capabilities,
      List<String> tags,
      bool enabled});
}

/// @nodoc
class _$SkillCopyWithImpl<$Res, $Val extends Skill>
    implements $SkillCopyWith<$Res> {
  _$SkillCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? slug = null,
    Object? name = null,
    Object? description = null,
    Object? category = null,
    Object? capabilities = null,
    Object? tags = null,
    Object? enabled = null,
  }) {
    return _then(_value.copyWith(
      slug: null == slug
          ? _value.slug
          : slug // ignore: cast_nullable_to_non_nullable
              as String,
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      category: null == category
          ? _value.category
          : category // ignore: cast_nullable_to_non_nullable
              as String,
      capabilities: null == capabilities
          ? _value.capabilities
          : capabilities // ignore: cast_nullable_to_non_nullable
              as List<String>,
      tags: null == tags
          ? _value.tags
          : tags // ignore: cast_nullable_to_non_nullable
              as List<String>,
      enabled: null == enabled
          ? _value.enabled
          : enabled // ignore: cast_nullable_to_non_nullable
              as bool,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$SkillImplCopyWith<$Res> implements $SkillCopyWith<$Res> {
  factory _$$SkillImplCopyWith(
          _$SkillImpl value, $Res Function(_$SkillImpl) then) =
      __$$SkillImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String slug,
      String name,
      String description,
      String category,
      List<String> capabilities,
      List<String> tags,
      bool enabled});
}

/// @nodoc
class __$$SkillImplCopyWithImpl<$Res>
    extends _$SkillCopyWithImpl<$Res, _$SkillImpl>
    implements _$$SkillImplCopyWith<$Res> {
  __$$SkillImplCopyWithImpl(
      _$SkillImpl _value, $Res Function(_$SkillImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? slug = null,
    Object? name = null,
    Object? description = null,
    Object? category = null,
    Object? capabilities = null,
    Object? tags = null,
    Object? enabled = null,
  }) {
    return _then(_$SkillImpl(
      slug: null == slug
          ? _value.slug
          : slug // ignore: cast_nullable_to_non_nullable
              as String,
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      category: null == category
          ? _value.category
          : category // ignore: cast_nullable_to_non_nullable
              as String,
      capabilities: null == capabilities
          ? _value._capabilities
          : capabilities // ignore: cast_nullable_to_non_nullable
              as List<String>,
      tags: null == tags
          ? _value._tags
          : tags // ignore: cast_nullable_to_non_nullable
              as List<String>,
      enabled: null == enabled
          ? _value.enabled
          : enabled // ignore: cast_nullable_to_non_nullable
              as bool,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$SkillImpl implements _Skill {
  const _$SkillImpl(
      {required this.slug,
      required this.name,
      required this.description,
      this.category = '',
      final List<String> capabilities = const [],
      final List<String> tags = const [],
      this.enabled = true})
      : _capabilities = capabilities,
        _tags = tags;

  factory _$SkillImpl.fromJson(Map<String, dynamic> json) =>
      _$$SkillImplFromJson(json);

  @override
  final String slug;
  @override
  final String name;
  @override
  final String description;
  @override
  @JsonKey()
  final String category;
  final List<String> _capabilities;
  @override
  @JsonKey()
  List<String> get capabilities {
    if (_capabilities is EqualUnmodifiableListView) return _capabilities;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(_capabilities);
  }

  final List<String> _tags;
  @override
  @JsonKey()
  List<String> get tags {
    if (_tags is EqualUnmodifiableListView) return _tags;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(_tags);
  }

  @override
  @JsonKey()
  final bool enabled;

  @override
  String toString() {
    return 'Skill(slug: $slug, name: $name, description: $description, category: $category, capabilities: $capabilities, tags: $tags, enabled: $enabled)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$SkillImpl &&
            (identical(other.slug, slug) || other.slug == slug) &&
            (identical(other.name, name) || other.name == name) &&
            (identical(other.description, description) ||
                other.description == description) &&
            (identical(other.category, category) ||
                other.category == category) &&
            const DeepCollectionEquality()
                .equals(other._capabilities, _capabilities) &&
            const DeepCollectionEquality().equals(other._tags, _tags) &&
            (identical(other.enabled, enabled) || other.enabled == enabled));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType,
      slug,
      name,
      description,
      category,
      const DeepCollectionEquality().hash(_capabilities),
      const DeepCollectionEquality().hash(_tags),
      enabled);

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$SkillImplCopyWith<_$SkillImpl> get copyWith =>
      __$$SkillImplCopyWithImpl<_$SkillImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$SkillImplToJson(
      this,
    );
  }
}

abstract class _Skill implements Skill {
  const factory _Skill(
      {required final String slug,
      required final String name,
      required final String description,
      final String category,
      final List<String> capabilities,
      final List<String> tags,
      final bool enabled}) = _$SkillImpl;

  factory _Skill.fromJson(Map<String, dynamic> json) = _$SkillImpl.fromJson;

  @override
  String get slug;
  @override
  String get name;
  @override
  String get description;
  @override
  String get category;
  @override
  List<String> get capabilities;
  @override
  List<String> get tags;
  @override
  bool get enabled;
  @override
  @JsonKey(ignore: true)
  _$$SkillImplCopyWith<_$SkillImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

MetricsSnapshot _$MetricsSnapshotFromJson(Map<String, dynamic> json) {
  return _MetricsSnapshot.fromJson(json);
}

/// @nodoc
mixin _$MetricsSnapshot {
  DateTime get timestamp => throw _privateConstructorUsedError;
  @JsonKey(name: 'active_agents')
  int get activeAgents => throw _privateConstructorUsedError;
  @JsonKey(name: 'requests_per_sec')
  double get requestsPerSec => throw _privateConstructorUsedError;
  @JsonKey(name: 'token_usage_rate')
  double get tokenUsageRate => throw _privateConstructorUsedError;
  @JsonKey(name: 'queue_depth')
  int get queueDepth => throw _privateConstructorUsedError;
  @JsonKey(name: 'total_sessions')
  int get totalSessions => throw _privateConstructorUsedError;
  @JsonKey(name: 'total_jobs')
  int get totalJobs => throw _privateConstructorUsedError;
  @JsonKey(name: 'running_jobs')
  int get runningJobs => throw _privateConstructorUsedError;
  @JsonKey(name: 'pending_jobs')
  int get pendingJobs => throw _privateConstructorUsedError;
  String get version => throw _privateConstructorUsedError;
  Map<String, dynamic>? get metadata => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $MetricsSnapshotCopyWith<MetricsSnapshot> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $MetricsSnapshotCopyWith<$Res> {
  factory $MetricsSnapshotCopyWith(
          MetricsSnapshot value, $Res Function(MetricsSnapshot) then) =
      _$MetricsSnapshotCopyWithImpl<$Res, MetricsSnapshot>;
  @useResult
  $Res call(
      {DateTime timestamp,
      @JsonKey(name: 'active_agents') int activeAgents,
      @JsonKey(name: 'requests_per_sec') double requestsPerSec,
      @JsonKey(name: 'token_usage_rate') double tokenUsageRate,
      @JsonKey(name: 'queue_depth') int queueDepth,
      @JsonKey(name: 'total_sessions') int totalSessions,
      @JsonKey(name: 'total_jobs') int totalJobs,
      @JsonKey(name: 'running_jobs') int runningJobs,
      @JsonKey(name: 'pending_jobs') int pendingJobs,
      String version,
      Map<String, dynamic>? metadata});
}

/// @nodoc
class _$MetricsSnapshotCopyWithImpl<$Res, $Val extends MetricsSnapshot>
    implements $MetricsSnapshotCopyWith<$Res> {
  _$MetricsSnapshotCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? timestamp = null,
    Object? activeAgents = null,
    Object? requestsPerSec = null,
    Object? tokenUsageRate = null,
    Object? queueDepth = null,
    Object? totalSessions = null,
    Object? totalJobs = null,
    Object? runningJobs = null,
    Object? pendingJobs = null,
    Object? version = null,
    Object? metadata = freezed,
  }) {
    return _then(_value.copyWith(
      timestamp: null == timestamp
          ? _value.timestamp
          : timestamp // ignore: cast_nullable_to_non_nullable
              as DateTime,
      activeAgents: null == activeAgents
          ? _value.activeAgents
          : activeAgents // ignore: cast_nullable_to_non_nullable
              as int,
      requestsPerSec: null == requestsPerSec
          ? _value.requestsPerSec
          : requestsPerSec // ignore: cast_nullable_to_non_nullable
              as double,
      tokenUsageRate: null == tokenUsageRate
          ? _value.tokenUsageRate
          : tokenUsageRate // ignore: cast_nullable_to_non_nullable
              as double,
      queueDepth: null == queueDepth
          ? _value.queueDepth
          : queueDepth // ignore: cast_nullable_to_non_nullable
              as int,
      totalSessions: null == totalSessions
          ? _value.totalSessions
          : totalSessions // ignore: cast_nullable_to_non_nullable
              as int,
      totalJobs: null == totalJobs
          ? _value.totalJobs
          : totalJobs // ignore: cast_nullable_to_non_nullable
              as int,
      runningJobs: null == runningJobs
          ? _value.runningJobs
          : runningJobs // ignore: cast_nullable_to_non_nullable
              as int,
      pendingJobs: null == pendingJobs
          ? _value.pendingJobs
          : pendingJobs // ignore: cast_nullable_to_non_nullable
              as int,
      version: null == version
          ? _value.version
          : version // ignore: cast_nullable_to_non_nullable
              as String,
      metadata: freezed == metadata
          ? _value.metadata
          : metadata // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$MetricsSnapshotImplCopyWith<$Res>
    implements $MetricsSnapshotCopyWith<$Res> {
  factory _$$MetricsSnapshotImplCopyWith(_$MetricsSnapshotImpl value,
          $Res Function(_$MetricsSnapshotImpl) then) =
      __$$MetricsSnapshotImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {DateTime timestamp,
      @JsonKey(name: 'active_agents') int activeAgents,
      @JsonKey(name: 'requests_per_sec') double requestsPerSec,
      @JsonKey(name: 'token_usage_rate') double tokenUsageRate,
      @JsonKey(name: 'queue_depth') int queueDepth,
      @JsonKey(name: 'total_sessions') int totalSessions,
      @JsonKey(name: 'total_jobs') int totalJobs,
      @JsonKey(name: 'running_jobs') int runningJobs,
      @JsonKey(name: 'pending_jobs') int pendingJobs,
      String version,
      Map<String, dynamic>? metadata});
}

/// @nodoc
class __$$MetricsSnapshotImplCopyWithImpl<$Res>
    extends _$MetricsSnapshotCopyWithImpl<$Res, _$MetricsSnapshotImpl>
    implements _$$MetricsSnapshotImplCopyWith<$Res> {
  __$$MetricsSnapshotImplCopyWithImpl(
      _$MetricsSnapshotImpl _value, $Res Function(_$MetricsSnapshotImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? timestamp = null,
    Object? activeAgents = null,
    Object? requestsPerSec = null,
    Object? tokenUsageRate = null,
    Object? queueDepth = null,
    Object? totalSessions = null,
    Object? totalJobs = null,
    Object? runningJobs = null,
    Object? pendingJobs = null,
    Object? version = null,
    Object? metadata = freezed,
  }) {
    return _then(_$MetricsSnapshotImpl(
      timestamp: null == timestamp
          ? _value.timestamp
          : timestamp // ignore: cast_nullable_to_non_nullable
              as DateTime,
      activeAgents: null == activeAgents
          ? _value.activeAgents
          : activeAgents // ignore: cast_nullable_to_non_nullable
              as int,
      requestsPerSec: null == requestsPerSec
          ? _value.requestsPerSec
          : requestsPerSec // ignore: cast_nullable_to_non_nullable
              as double,
      tokenUsageRate: null == tokenUsageRate
          ? _value.tokenUsageRate
          : tokenUsageRate // ignore: cast_nullable_to_non_nullable
              as double,
      queueDepth: null == queueDepth
          ? _value.queueDepth
          : queueDepth // ignore: cast_nullable_to_non_nullable
              as int,
      totalSessions: null == totalSessions
          ? _value.totalSessions
          : totalSessions // ignore: cast_nullable_to_non_nullable
              as int,
      totalJobs: null == totalJobs
          ? _value.totalJobs
          : totalJobs // ignore: cast_nullable_to_non_nullable
              as int,
      runningJobs: null == runningJobs
          ? _value.runningJobs
          : runningJobs // ignore: cast_nullable_to_non_nullable
              as int,
      pendingJobs: null == pendingJobs
          ? _value.pendingJobs
          : pendingJobs // ignore: cast_nullable_to_non_nullable
              as int,
      version: null == version
          ? _value.version
          : version // ignore: cast_nullable_to_non_nullable
              as String,
      metadata: freezed == metadata
          ? _value._metadata
          : metadata // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$MetricsSnapshotImpl implements _MetricsSnapshot {
  const _$MetricsSnapshotImpl(
      {required this.timestamp,
      @JsonKey(name: 'active_agents') this.activeAgents = 0,
      @JsonKey(name: 'requests_per_sec') this.requestsPerSec = 0.0,
      @JsonKey(name: 'token_usage_rate') this.tokenUsageRate = 0.0,
      @JsonKey(name: 'queue_depth') this.queueDepth = 0,
      @JsonKey(name: 'total_sessions') this.totalSessions = 0,
      @JsonKey(name: 'total_jobs') this.totalJobs = 0,
      @JsonKey(name: 'running_jobs') this.runningJobs = 0,
      @JsonKey(name: 'pending_jobs') this.pendingJobs = 0,
      this.version = '',
      final Map<String, dynamic>? metadata})
      : _metadata = metadata;

  factory _$MetricsSnapshotImpl.fromJson(Map<String, dynamic> json) =>
      _$$MetricsSnapshotImplFromJson(json);

  @override
  final DateTime timestamp;
  @override
  @JsonKey(name: 'active_agents')
  final int activeAgents;
  @override
  @JsonKey(name: 'requests_per_sec')
  final double requestsPerSec;
  @override
  @JsonKey(name: 'token_usage_rate')
  final double tokenUsageRate;
  @override
  @JsonKey(name: 'queue_depth')
  final int queueDepth;
  @override
  @JsonKey(name: 'total_sessions')
  final int totalSessions;
  @override
  @JsonKey(name: 'total_jobs')
  final int totalJobs;
  @override
  @JsonKey(name: 'running_jobs')
  final int runningJobs;
  @override
  @JsonKey(name: 'pending_jobs')
  final int pendingJobs;
  @override
  @JsonKey()
  final String version;
  final Map<String, dynamic>? _metadata;
  @override
  Map<String, dynamic>? get metadata {
    final value = _metadata;
    if (value == null) return null;
    if (_metadata is EqualUnmodifiableMapView) return _metadata;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableMapView(value);
  }

  @override
  String toString() {
    return 'MetricsSnapshot(timestamp: $timestamp, activeAgents: $activeAgents, requestsPerSec: $requestsPerSec, tokenUsageRate: $tokenUsageRate, queueDepth: $queueDepth, totalSessions: $totalSessions, totalJobs: $totalJobs, runningJobs: $runningJobs, pendingJobs: $pendingJobs, version: $version, metadata: $metadata)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$MetricsSnapshotImpl &&
            (identical(other.timestamp, timestamp) ||
                other.timestamp == timestamp) &&
            (identical(other.activeAgents, activeAgents) ||
                other.activeAgents == activeAgents) &&
            (identical(other.requestsPerSec, requestsPerSec) ||
                other.requestsPerSec == requestsPerSec) &&
            (identical(other.tokenUsageRate, tokenUsageRate) ||
                other.tokenUsageRate == tokenUsageRate) &&
            (identical(other.queueDepth, queueDepth) ||
                other.queueDepth == queueDepth) &&
            (identical(other.totalSessions, totalSessions) ||
                other.totalSessions == totalSessions) &&
            (identical(other.totalJobs, totalJobs) ||
                other.totalJobs == totalJobs) &&
            (identical(other.runningJobs, runningJobs) ||
                other.runningJobs == runningJobs) &&
            (identical(other.pendingJobs, pendingJobs) ||
                other.pendingJobs == pendingJobs) &&
            (identical(other.version, version) || other.version == version) &&
            const DeepCollectionEquality().equals(other._metadata, _metadata));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType,
      timestamp,
      activeAgents,
      requestsPerSec,
      tokenUsageRate,
      queueDepth,
      totalSessions,
      totalJobs,
      runningJobs,
      pendingJobs,
      version,
      const DeepCollectionEquality().hash(_metadata));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$MetricsSnapshotImplCopyWith<_$MetricsSnapshotImpl> get copyWith =>
      __$$MetricsSnapshotImplCopyWithImpl<_$MetricsSnapshotImpl>(
          this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$MetricsSnapshotImplToJson(
      this,
    );
  }
}

abstract class _MetricsSnapshot implements MetricsSnapshot {
  const factory _MetricsSnapshot(
      {required final DateTime timestamp,
      @JsonKey(name: 'active_agents') final int activeAgents,
      @JsonKey(name: 'requests_per_sec') final double requestsPerSec,
      @JsonKey(name: 'token_usage_rate') final double tokenUsageRate,
      @JsonKey(name: 'queue_depth') final int queueDepth,
      @JsonKey(name: 'total_sessions') final int totalSessions,
      @JsonKey(name: 'total_jobs') final int totalJobs,
      @JsonKey(name: 'running_jobs') final int runningJobs,
      @JsonKey(name: 'pending_jobs') final int pendingJobs,
      final String version,
      final Map<String, dynamic>? metadata}) = _$MetricsSnapshotImpl;

  factory _MetricsSnapshot.fromJson(Map<String, dynamic> json) =
      _$MetricsSnapshotImpl.fromJson;

  @override
  DateTime get timestamp;
  @override
  @JsonKey(name: 'active_agents')
  int get activeAgents;
  @override
  @JsonKey(name: 'requests_per_sec')
  double get requestsPerSec;
  @override
  @JsonKey(name: 'token_usage_rate')
  double get tokenUsageRate;
  @override
  @JsonKey(name: 'queue_depth')
  int get queueDepth;
  @override
  @JsonKey(name: 'total_sessions')
  int get totalSessions;
  @override
  @JsonKey(name: 'total_jobs')
  int get totalJobs;
  @override
  @JsonKey(name: 'running_jobs')
  int get runningJobs;
  @override
  @JsonKey(name: 'pending_jobs')
  int get pendingJobs;
  @override
  String get version;
  @override
  Map<String, dynamic>? get metadata;
  @override
  @JsonKey(ignore: true)
  _$$MetricsSnapshotImplCopyWith<_$MetricsSnapshotImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

Plan _$PlanFromJson(Map<String, dynamic> json) {
  return _Plan.fromJson(json);
}

/// @nodoc
mixin _$Plan {
  String get id => throw _privateConstructorUsedError;
  String get title => throw _privateConstructorUsedError;
  String get description => throw _privateConstructorUsedError;
  @JsonKey(name: 'file_path')
  String get filePath => throw _privateConstructorUsedError;
  @JsonKey(name: 'project_id')
  String? get projectID => throw _privateConstructorUsedError;
  String get state => throw _privateConstructorUsedError;
  @JsonKey(name: 'created_at')
  DateTime get createdAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'updated_at')
  DateTime get updatedAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'approved_at')
  DateTime? get approvedAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'confirmed_at')
  DateTime? get confirmedAt => throw _privateConstructorUsedError;
  @JsonKey(name: 'approved_by')
  String? get approvedBy => throw _privateConstructorUsedError;
  @JsonKey(name: 'confirmed_by')
  String? get confirmedBy => throw _privateConstructorUsedError;
  @JsonKey(name: 'task_id')
  String? get taskID => throw _privateConstructorUsedError;
  @JsonKey(name: 'source_session')
  String? get sourceSession => throw _privateConstructorUsedError;
  @JsonKey(name: 'revision_count')
  int get revisionCount => throw _privateConstructorUsedError;
  List<PlanPhase> get phases => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $PlanCopyWith<Plan> get copyWith => throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $PlanCopyWith<$Res> {
  factory $PlanCopyWith(Plan value, $Res Function(Plan) then) =
      _$PlanCopyWithImpl<$Res, Plan>;
  @useResult
  $Res call(
      {String id,
      String title,
      String description,
      @JsonKey(name: 'file_path') String filePath,
      @JsonKey(name: 'project_id') String? projectID,
      String state,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'updated_at') DateTime updatedAt,
      @JsonKey(name: 'approved_at') DateTime? approvedAt,
      @JsonKey(name: 'confirmed_at') DateTime? confirmedAt,
      @JsonKey(name: 'approved_by') String? approvedBy,
      @JsonKey(name: 'confirmed_by') String? confirmedBy,
      @JsonKey(name: 'task_id') String? taskID,
      @JsonKey(name: 'source_session') String? sourceSession,
      @JsonKey(name: 'revision_count') int revisionCount,
      List<PlanPhase> phases});
}

/// @nodoc
class _$PlanCopyWithImpl<$Res, $Val extends Plan>
    implements $PlanCopyWith<$Res> {
  _$PlanCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? title = null,
    Object? description = null,
    Object? filePath = null,
    Object? projectID = freezed,
    Object? state = null,
    Object? createdAt = null,
    Object? updatedAt = null,
    Object? approvedAt = freezed,
    Object? confirmedAt = freezed,
    Object? approvedBy = freezed,
    Object? confirmedBy = freezed,
    Object? taskID = freezed,
    Object? sourceSession = freezed,
    Object? revisionCount = null,
    Object? phases = null,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      filePath: null == filePath
          ? _value.filePath
          : filePath // ignore: cast_nullable_to_non_nullable
              as String,
      projectID: freezed == projectID
          ? _value.projectID
          : projectID // ignore: cast_nullable_to_non_nullable
              as String?,
      state: null == state
          ? _value.state
          : state // ignore: cast_nullable_to_non_nullable
              as String,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      updatedAt: null == updatedAt
          ? _value.updatedAt
          : updatedAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      approvedAt: freezed == approvedAt
          ? _value.approvedAt
          : approvedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      confirmedAt: freezed == confirmedAt
          ? _value.confirmedAt
          : confirmedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      approvedBy: freezed == approvedBy
          ? _value.approvedBy
          : approvedBy // ignore: cast_nullable_to_non_nullable
              as String?,
      confirmedBy: freezed == confirmedBy
          ? _value.confirmedBy
          : confirmedBy // ignore: cast_nullable_to_non_nullable
              as String?,
      taskID: freezed == taskID
          ? _value.taskID
          : taskID // ignore: cast_nullable_to_non_nullable
              as String?,
      sourceSession: freezed == sourceSession
          ? _value.sourceSession
          : sourceSession // ignore: cast_nullable_to_non_nullable
              as String?,
      revisionCount: null == revisionCount
          ? _value.revisionCount
          : revisionCount // ignore: cast_nullable_to_non_nullable
              as int,
      phases: null == phases
          ? _value.phases
          : phases // ignore: cast_nullable_to_non_nullable
              as List<PlanPhase>,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$PlanImplCopyWith<$Res> implements $PlanCopyWith<$Res> {
  factory _$$PlanImplCopyWith(
          _$PlanImpl value, $Res Function(_$PlanImpl) then) =
      __$$PlanImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      String title,
      String description,
      @JsonKey(name: 'file_path') String filePath,
      @JsonKey(name: 'project_id') String? projectID,
      String state,
      @JsonKey(name: 'created_at') DateTime createdAt,
      @JsonKey(name: 'updated_at') DateTime updatedAt,
      @JsonKey(name: 'approved_at') DateTime? approvedAt,
      @JsonKey(name: 'confirmed_at') DateTime? confirmedAt,
      @JsonKey(name: 'approved_by') String? approvedBy,
      @JsonKey(name: 'confirmed_by') String? confirmedBy,
      @JsonKey(name: 'task_id') String? taskID,
      @JsonKey(name: 'source_session') String? sourceSession,
      @JsonKey(name: 'revision_count') int revisionCount,
      List<PlanPhase> phases});
}

/// @nodoc
class __$$PlanImplCopyWithImpl<$Res>
    extends _$PlanCopyWithImpl<$Res, _$PlanImpl>
    implements _$$PlanImplCopyWith<$Res> {
  __$$PlanImplCopyWithImpl(_$PlanImpl _value, $Res Function(_$PlanImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? title = null,
    Object? description = null,
    Object? filePath = null,
    Object? projectID = freezed,
    Object? state = null,
    Object? createdAt = null,
    Object? updatedAt = null,
    Object? approvedAt = freezed,
    Object? confirmedAt = freezed,
    Object? approvedBy = freezed,
    Object? confirmedBy = freezed,
    Object? taskID = freezed,
    Object? sourceSession = freezed,
    Object? revisionCount = null,
    Object? phases = null,
  }) {
    return _then(_$PlanImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      description: null == description
          ? _value.description
          : description // ignore: cast_nullable_to_non_nullable
              as String,
      filePath: null == filePath
          ? _value.filePath
          : filePath // ignore: cast_nullable_to_non_nullable
              as String,
      projectID: freezed == projectID
          ? _value.projectID
          : projectID // ignore: cast_nullable_to_non_nullable
              as String?,
      state: null == state
          ? _value.state
          : state // ignore: cast_nullable_to_non_nullable
              as String,
      createdAt: null == createdAt
          ? _value.createdAt
          : createdAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      updatedAt: null == updatedAt
          ? _value.updatedAt
          : updatedAt // ignore: cast_nullable_to_non_nullable
              as DateTime,
      approvedAt: freezed == approvedAt
          ? _value.approvedAt
          : approvedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      confirmedAt: freezed == confirmedAt
          ? _value.confirmedAt
          : confirmedAt // ignore: cast_nullable_to_non_nullable
              as DateTime?,
      approvedBy: freezed == approvedBy
          ? _value.approvedBy
          : approvedBy // ignore: cast_nullable_to_non_nullable
              as String?,
      confirmedBy: freezed == confirmedBy
          ? _value.confirmedBy
          : confirmedBy // ignore: cast_nullable_to_non_nullable
              as String?,
      taskID: freezed == taskID
          ? _value.taskID
          : taskID // ignore: cast_nullable_to_non_nullable
              as String?,
      sourceSession: freezed == sourceSession
          ? _value.sourceSession
          : sourceSession // ignore: cast_nullable_to_non_nullable
              as String?,
      revisionCount: null == revisionCount
          ? _value.revisionCount
          : revisionCount // ignore: cast_nullable_to_non_nullable
              as int,
      phases: null == phases
          ? _value._phases
          : phases // ignore: cast_nullable_to_non_nullable
              as List<PlanPhase>,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$PlanImpl implements _Plan {
  const _$PlanImpl(
      {required this.id,
      required this.title,
      this.description = '',
      @JsonKey(name: 'file_path') this.filePath = '',
      @JsonKey(name: 'project_id') this.projectID,
      required this.state,
      @JsonKey(name: 'created_at') required this.createdAt,
      @JsonKey(name: 'updated_at') required this.updatedAt,
      @JsonKey(name: 'approved_at') this.approvedAt,
      @JsonKey(name: 'confirmed_at') this.confirmedAt,
      @JsonKey(name: 'approved_by') this.approvedBy,
      @JsonKey(name: 'confirmed_by') this.confirmedBy,
      @JsonKey(name: 'task_id') this.taskID,
      @JsonKey(name: 'source_session') this.sourceSession,
      @JsonKey(name: 'revision_count') this.revisionCount = 0,
      final List<PlanPhase> phases = const []})
      : _phases = phases;

  factory _$PlanImpl.fromJson(Map<String, dynamic> json) =>
      _$$PlanImplFromJson(json);

  @override
  final String id;
  @override
  final String title;
  @override
  @JsonKey()
  final String description;
  @override
  @JsonKey(name: 'file_path')
  final String filePath;
  @override
  @JsonKey(name: 'project_id')
  final String? projectID;
  @override
  final String state;
  @override
  @JsonKey(name: 'created_at')
  final DateTime createdAt;
  @override
  @JsonKey(name: 'updated_at')
  final DateTime updatedAt;
  @override
  @JsonKey(name: 'approved_at')
  final DateTime? approvedAt;
  @override
  @JsonKey(name: 'confirmed_at')
  final DateTime? confirmedAt;
  @override
  @JsonKey(name: 'approved_by')
  final String? approvedBy;
  @override
  @JsonKey(name: 'confirmed_by')
  final String? confirmedBy;
  @override
  @JsonKey(name: 'task_id')
  final String? taskID;
  @override
  @JsonKey(name: 'source_session')
  final String? sourceSession;
  @override
  @JsonKey(name: 'revision_count')
  final int revisionCount;
  final List<PlanPhase> _phases;
  @override
  @JsonKey()
  List<PlanPhase> get phases {
    if (_phases is EqualUnmodifiableListView) return _phases;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(_phases);
  }

  @override
  String toString() {
    return 'Plan(id: $id, title: $title, description: $description, filePath: $filePath, projectID: $projectID, state: $state, createdAt: $createdAt, updatedAt: $updatedAt, approvedAt: $approvedAt, confirmedAt: $confirmedAt, approvedBy: $approvedBy, confirmedBy: $confirmedBy, taskID: $taskID, sourceSession: $sourceSession, revisionCount: $revisionCount, phases: $phases)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$PlanImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.title, title) || other.title == title) &&
            (identical(other.description, description) ||
                other.description == description) &&
            (identical(other.filePath, filePath) ||
                other.filePath == filePath) &&
            (identical(other.projectID, projectID) ||
                other.projectID == projectID) &&
            (identical(other.state, state) || other.state == state) &&
            (identical(other.createdAt, createdAt) ||
                other.createdAt == createdAt) &&
            (identical(other.updatedAt, updatedAt) ||
                other.updatedAt == updatedAt) &&
            (identical(other.approvedAt, approvedAt) ||
                other.approvedAt == approvedAt) &&
            (identical(other.confirmedAt, confirmedAt) ||
                other.confirmedAt == confirmedAt) &&
            (identical(other.approvedBy, approvedBy) ||
                other.approvedBy == approvedBy) &&
            (identical(other.confirmedBy, confirmedBy) ||
                other.confirmedBy == confirmedBy) &&
            (identical(other.taskID, taskID) || other.taskID == taskID) &&
            (identical(other.sourceSession, sourceSession) ||
                other.sourceSession == sourceSession) &&
            (identical(other.revisionCount, revisionCount) ||
                other.revisionCount == revisionCount) &&
            const DeepCollectionEquality().equals(other._phases, _phases));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType,
      id,
      title,
      description,
      filePath,
      projectID,
      state,
      createdAt,
      updatedAt,
      approvedAt,
      confirmedAt,
      approvedBy,
      confirmedBy,
      taskID,
      sourceSession,
      revisionCount,
      const DeepCollectionEquality().hash(_phases));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$PlanImplCopyWith<_$PlanImpl> get copyWith =>
      __$$PlanImplCopyWithImpl<_$PlanImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$PlanImplToJson(
      this,
    );
  }
}

abstract class _Plan implements Plan {
  const factory _Plan(
      {required final String id,
      required final String title,
      final String description,
      @JsonKey(name: 'file_path') final String filePath,
      @JsonKey(name: 'project_id') final String? projectID,
      required final String state,
      @JsonKey(name: 'created_at') required final DateTime createdAt,
      @JsonKey(name: 'updated_at') required final DateTime updatedAt,
      @JsonKey(name: 'approved_at') final DateTime? approvedAt,
      @JsonKey(name: 'confirmed_at') final DateTime? confirmedAt,
      @JsonKey(name: 'approved_by') final String? approvedBy,
      @JsonKey(name: 'confirmed_by') final String? confirmedBy,
      @JsonKey(name: 'task_id') final String? taskID,
      @JsonKey(name: 'source_session') final String? sourceSession,
      @JsonKey(name: 'revision_count') final int revisionCount,
      final List<PlanPhase> phases}) = _$PlanImpl;

  factory _Plan.fromJson(Map<String, dynamic> json) = _$PlanImpl.fromJson;

  @override
  String get id;
  @override
  String get title;
  @override
  String get description;
  @override
  @JsonKey(name: 'file_path')
  String get filePath;
  @override
  @JsonKey(name: 'project_id')
  String? get projectID;
  @override
  String get state;
  @override
  @JsonKey(name: 'created_at')
  DateTime get createdAt;
  @override
  @JsonKey(name: 'updated_at')
  DateTime get updatedAt;
  @override
  @JsonKey(name: 'approved_at')
  DateTime? get approvedAt;
  @override
  @JsonKey(name: 'confirmed_at')
  DateTime? get confirmedAt;
  @override
  @JsonKey(name: 'approved_by')
  String? get approvedBy;
  @override
  @JsonKey(name: 'confirmed_by')
  String? get confirmedBy;
  @override
  @JsonKey(name: 'task_id')
  String? get taskID;
  @override
  @JsonKey(name: 'source_session')
  String? get sourceSession;
  @override
  @JsonKey(name: 'revision_count')
  int get revisionCount;
  @override
  List<PlanPhase> get phases;
  @override
  @JsonKey(ignore: true)
  _$$PlanImplCopyWith<_$PlanImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

PlanPhase _$PlanPhaseFromJson(Map<String, dynamic> json) {
  return _PlanPhase.fromJson(json);
}

/// @nodoc
mixin _$PlanPhase {
  String get id => throw _privateConstructorUsedError;
  @JsonKey(name: 'plan_id')
  String get planID => throw _privateConstructorUsedError;
  String get name => throw _privateConstructorUsedError;
  int get sequence => throw _privateConstructorUsedError;
  @JsonKey(name: 'total_steps')
  int get totalSteps => throw _privateConstructorUsedError;
  @JsonKey(name: 'completed_steps')
  int get completedSteps => throw _privateConstructorUsedError;
  @JsonKey(name: 'failed_steps')
  int get failedSteps => throw _privateConstructorUsedError;
  String get state => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $PlanPhaseCopyWith<PlanPhase> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $PlanPhaseCopyWith<$Res> {
  factory $PlanPhaseCopyWith(PlanPhase value, $Res Function(PlanPhase) then) =
      _$PlanPhaseCopyWithImpl<$Res, PlanPhase>;
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'plan_id') String planID,
      String name,
      int sequence,
      @JsonKey(name: 'total_steps') int totalSteps,
      @JsonKey(name: 'completed_steps') int completedSteps,
      @JsonKey(name: 'failed_steps') int failedSteps,
      String state});
}

/// @nodoc
class _$PlanPhaseCopyWithImpl<$Res, $Val extends PlanPhase>
    implements $PlanPhaseCopyWith<$Res> {
  _$PlanPhaseCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? planID = null,
    Object? name = null,
    Object? sequence = null,
    Object? totalSteps = null,
    Object? completedSteps = null,
    Object? failedSteps = null,
    Object? state = null,
  }) {
    return _then(_value.copyWith(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      planID: null == planID
          ? _value.planID
          : planID // ignore: cast_nullable_to_non_nullable
              as String,
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      sequence: null == sequence
          ? _value.sequence
          : sequence // ignore: cast_nullable_to_non_nullable
              as int,
      totalSteps: null == totalSteps
          ? _value.totalSteps
          : totalSteps // ignore: cast_nullable_to_non_nullable
              as int,
      completedSteps: null == completedSteps
          ? _value.completedSteps
          : completedSteps // ignore: cast_nullable_to_non_nullable
              as int,
      failedSteps: null == failedSteps
          ? _value.failedSteps
          : failedSteps // ignore: cast_nullable_to_non_nullable
              as int,
      state: null == state
          ? _value.state
          : state // ignore: cast_nullable_to_non_nullable
              as String,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$PlanPhaseImplCopyWith<$Res>
    implements $PlanPhaseCopyWith<$Res> {
  factory _$$PlanPhaseImplCopyWith(
          _$PlanPhaseImpl value, $Res Function(_$PlanPhaseImpl) then) =
      __$$PlanPhaseImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String id,
      @JsonKey(name: 'plan_id') String planID,
      String name,
      int sequence,
      @JsonKey(name: 'total_steps') int totalSteps,
      @JsonKey(name: 'completed_steps') int completedSteps,
      @JsonKey(name: 'failed_steps') int failedSteps,
      String state});
}

/// @nodoc
class __$$PlanPhaseImplCopyWithImpl<$Res>
    extends _$PlanPhaseCopyWithImpl<$Res, _$PlanPhaseImpl>
    implements _$$PlanPhaseImplCopyWith<$Res> {
  __$$PlanPhaseImplCopyWithImpl(
      _$PlanPhaseImpl _value, $Res Function(_$PlanPhaseImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? id = null,
    Object? planID = null,
    Object? name = null,
    Object? sequence = null,
    Object? totalSteps = null,
    Object? completedSteps = null,
    Object? failedSteps = null,
    Object? state = null,
  }) {
    return _then(_$PlanPhaseImpl(
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      planID: null == planID
          ? _value.planID
          : planID // ignore: cast_nullable_to_non_nullable
              as String,
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      sequence: null == sequence
          ? _value.sequence
          : sequence // ignore: cast_nullable_to_non_nullable
              as int,
      totalSteps: null == totalSteps
          ? _value.totalSteps
          : totalSteps // ignore: cast_nullable_to_non_nullable
              as int,
      completedSteps: null == completedSteps
          ? _value.completedSteps
          : completedSteps // ignore: cast_nullable_to_non_nullable
              as int,
      failedSteps: null == failedSteps
          ? _value.failedSteps
          : failedSteps // ignore: cast_nullable_to_non_nullable
              as int,
      state: null == state
          ? _value.state
          : state // ignore: cast_nullable_to_non_nullable
              as String,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$PlanPhaseImpl implements _PlanPhase {
  const _$PlanPhaseImpl(
      {required this.id,
      @JsonKey(name: 'plan_id') required this.planID,
      required this.name,
      required this.sequence,
      @JsonKey(name: 'total_steps') this.totalSteps = 0,
      @JsonKey(name: 'completed_steps') this.completedSteps = 0,
      @JsonKey(name: 'failed_steps') this.failedSteps = 0,
      required this.state});

  factory _$PlanPhaseImpl.fromJson(Map<String, dynamic> json) =>
      _$$PlanPhaseImplFromJson(json);

  @override
  final String id;
  @override
  @JsonKey(name: 'plan_id')
  final String planID;
  @override
  final String name;
  @override
  final int sequence;
  @override
  @JsonKey(name: 'total_steps')
  final int totalSteps;
  @override
  @JsonKey(name: 'completed_steps')
  final int completedSteps;
  @override
  @JsonKey(name: 'failed_steps')
  final int failedSteps;
  @override
  final String state;

  @override
  String toString() {
    return 'PlanPhase(id: $id, planID: $planID, name: $name, sequence: $sequence, totalSteps: $totalSteps, completedSteps: $completedSteps, failedSteps: $failedSteps, state: $state)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$PlanPhaseImpl &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.planID, planID) || other.planID == planID) &&
            (identical(other.name, name) || other.name == name) &&
            (identical(other.sequence, sequence) ||
                other.sequence == sequence) &&
            (identical(other.totalSteps, totalSteps) ||
                other.totalSteps == totalSteps) &&
            (identical(other.completedSteps, completedSteps) ||
                other.completedSteps == completedSteps) &&
            (identical(other.failedSteps, failedSteps) ||
                other.failedSteps == failedSteps) &&
            (identical(other.state, state) || other.state == state));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, id, planID, name, sequence,
      totalSteps, completedSteps, failedSteps, state);

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$PlanPhaseImplCopyWith<_$PlanPhaseImpl> get copyWith =>
      __$$PlanPhaseImplCopyWithImpl<_$PlanPhaseImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$PlanPhaseImplToJson(
      this,
    );
  }
}

abstract class _PlanPhase implements PlanPhase {
  const factory _PlanPhase(
      {required final String id,
      @JsonKey(name: 'plan_id') required final String planID,
      required final String name,
      required final int sequence,
      @JsonKey(name: 'total_steps') final int totalSteps,
      @JsonKey(name: 'completed_steps') final int completedSteps,
      @JsonKey(name: 'failed_steps') final int failedSteps,
      required final String state}) = _$PlanPhaseImpl;

  factory _PlanPhase.fromJson(Map<String, dynamic> json) =
      _$PlanPhaseImpl.fromJson;

  @override
  String get id;
  @override
  @JsonKey(name: 'plan_id')
  String get planID;
  @override
  String get name;
  @override
  int get sequence;
  @override
  @JsonKey(name: 'total_steps')
  int get totalSteps;
  @override
  @JsonKey(name: 'completed_steps')
  int get completedSteps;
  @override
  @JsonKey(name: 'failed_steps')
  int get failedSteps;
  @override
  String get state;
  @override
  @JsonKey(ignore: true)
  _$$PlanPhaseImplCopyWith<_$PlanPhaseImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

SearchResults _$SearchResultsFromJson(Map<String, dynamic> json) {
  return _SearchResults.fromJson(json);
}

/// @nodoc
mixin _$SearchResults {
  List<SearchResultItem> get results => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $SearchResultsCopyWith<SearchResults> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $SearchResultsCopyWith<$Res> {
  factory $SearchResultsCopyWith(
          SearchResults value, $Res Function(SearchResults) then) =
      _$SearchResultsCopyWithImpl<$Res, SearchResults>;
  @useResult
  $Res call({List<SearchResultItem> results});
}

/// @nodoc
class _$SearchResultsCopyWithImpl<$Res, $Val extends SearchResults>
    implements $SearchResultsCopyWith<$Res> {
  _$SearchResultsCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? results = null,
  }) {
    return _then(_value.copyWith(
      results: null == results
          ? _value.results
          : results // ignore: cast_nullable_to_non_nullable
              as List<SearchResultItem>,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$SearchResultsImplCopyWith<$Res>
    implements $SearchResultsCopyWith<$Res> {
  factory _$$SearchResultsImplCopyWith(
          _$SearchResultsImpl value, $Res Function(_$SearchResultsImpl) then) =
      __$$SearchResultsImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call({List<SearchResultItem> results});
}

/// @nodoc
class __$$SearchResultsImplCopyWithImpl<$Res>
    extends _$SearchResultsCopyWithImpl<$Res, _$SearchResultsImpl>
    implements _$$SearchResultsImplCopyWith<$Res> {
  __$$SearchResultsImplCopyWithImpl(
      _$SearchResultsImpl _value, $Res Function(_$SearchResultsImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? results = null,
  }) {
    return _then(_$SearchResultsImpl(
      results: null == results
          ? _value._results
          : results // ignore: cast_nullable_to_non_nullable
              as List<SearchResultItem>,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$SearchResultsImpl implements _SearchResults {
  const _$SearchResultsImpl({final List<SearchResultItem> results = const []})
      : _results = results;

  factory _$SearchResultsImpl.fromJson(Map<String, dynamic> json) =>
      _$$SearchResultsImplFromJson(json);

  final List<SearchResultItem> _results;
  @override
  @JsonKey()
  List<SearchResultItem> get results {
    if (_results is EqualUnmodifiableListView) return _results;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(_results);
  }

  @override
  String toString() {
    return 'SearchResults(results: $results)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$SearchResultsImpl &&
            const DeepCollectionEquality().equals(other._results, _results));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode =>
      Object.hash(runtimeType, const DeepCollectionEquality().hash(_results));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$SearchResultsImplCopyWith<_$SearchResultsImpl> get copyWith =>
      __$$SearchResultsImplCopyWithImpl<_$SearchResultsImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$SearchResultsImplToJson(
      this,
    );
  }
}

abstract class _SearchResults implements SearchResults {
  const factory _SearchResults({final List<SearchResultItem> results}) =
      _$SearchResultsImpl;

  factory _SearchResults.fromJson(Map<String, dynamic> json) =
      _$SearchResultsImpl.fromJson;

  @override
  List<SearchResultItem> get results;
  @override
  @JsonKey(ignore: true)
  _$$SearchResultsImplCopyWith<_$SearchResultsImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

SearchResultItem _$SearchResultItemFromJson(Map<String, dynamic> json) {
  return _SearchResultItem.fromJson(json);
}

/// @nodoc
mixin _$SearchResultItem {
  SearchResultType get type => throw _privateConstructorUsedError;
  String get id => throw _privateConstructorUsedError;
  String get title => throw _privateConstructorUsedError;
  String get snippet => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $SearchResultItemCopyWith<SearchResultItem> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $SearchResultItemCopyWith<$Res> {
  factory $SearchResultItemCopyWith(
          SearchResultItem value, $Res Function(SearchResultItem) then) =
      _$SearchResultItemCopyWithImpl<$Res, SearchResultItem>;
  @useResult
  $Res call({SearchResultType type, String id, String title, String snippet});
}

/// @nodoc
class _$SearchResultItemCopyWithImpl<$Res, $Val extends SearchResultItem>
    implements $SearchResultItemCopyWith<$Res> {
  _$SearchResultItemCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? type = null,
    Object? id = null,
    Object? title = null,
    Object? snippet = null,
  }) {
    return _then(_value.copyWith(
      type: null == type
          ? _value.type
          : type // ignore: cast_nullable_to_non_nullable
              as SearchResultType,
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      snippet: null == snippet
          ? _value.snippet
          : snippet // ignore: cast_nullable_to_non_nullable
              as String,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$SearchResultItemImplCopyWith<$Res>
    implements $SearchResultItemCopyWith<$Res> {
  factory _$$SearchResultItemImplCopyWith(_$SearchResultItemImpl value,
          $Res Function(_$SearchResultItemImpl) then) =
      __$$SearchResultItemImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call({SearchResultType type, String id, String title, String snippet});
}

/// @nodoc
class __$$SearchResultItemImplCopyWithImpl<$Res>
    extends _$SearchResultItemCopyWithImpl<$Res, _$SearchResultItemImpl>
    implements _$$SearchResultItemImplCopyWith<$Res> {
  __$$SearchResultItemImplCopyWithImpl(_$SearchResultItemImpl _value,
      $Res Function(_$SearchResultItemImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? type = null,
    Object? id = null,
    Object? title = null,
    Object? snippet = null,
  }) {
    return _then(_$SearchResultItemImpl(
      type: null == type
          ? _value.type
          : type // ignore: cast_nullable_to_non_nullable
              as SearchResultType,
      id: null == id
          ? _value.id
          : id // ignore: cast_nullable_to_non_nullable
              as String,
      title: null == title
          ? _value.title
          : title // ignore: cast_nullable_to_non_nullable
              as String,
      snippet: null == snippet
          ? _value.snippet
          : snippet // ignore: cast_nullable_to_non_nullable
              as String,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$SearchResultItemImpl implements _SearchResultItem {
  const _$SearchResultItemImpl(
      {required this.type,
      required this.id,
      required this.title,
      this.snippet = ''});

  factory _$SearchResultItemImpl.fromJson(Map<String, dynamic> json) =>
      _$$SearchResultItemImplFromJson(json);

  @override
  final SearchResultType type;
  @override
  final String id;
  @override
  final String title;
  @override
  @JsonKey()
  final String snippet;

  @override
  String toString() {
    return 'SearchResultItem(type: $type, id: $id, title: $title, snippet: $snippet)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$SearchResultItemImpl &&
            (identical(other.type, type) || other.type == type) &&
            (identical(other.id, id) || other.id == id) &&
            (identical(other.title, title) || other.title == title) &&
            (identical(other.snippet, snippet) || other.snippet == snippet));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, type, id, title, snippet);

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$SearchResultItemImplCopyWith<_$SearchResultItemImpl> get copyWith =>
      __$$SearchResultItemImplCopyWithImpl<_$SearchResultItemImpl>(
          this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$SearchResultItemImplToJson(
      this,
    );
  }
}

abstract class _SearchResultItem implements SearchResultItem {
  const factory _SearchResultItem(
      {required final SearchResultType type,
      required final String id,
      required final String title,
      final String snippet}) = _$SearchResultItemImpl;

  factory _SearchResultItem.fromJson(Map<String, dynamic> json) =
      _$SearchResultItemImpl.fromJson;

  @override
  SearchResultType get type;
  @override
  String get id;
  @override
  String get title;
  @override
  String get snippet;
  @override
  @JsonKey(ignore: true)
  _$$SearchResultItemImplCopyWith<_$SearchResultItemImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

BranchInfo _$BranchInfoFromJson(Map<String, dynamic> json) {
  return _BranchInfo.fromJson(json);
}

/// @nodoc
mixin _$BranchInfo {
  String get name => throw _privateConstructorUsedError;
  @JsonKey(name: 'is_current')
  bool get isCurrent => throw _privateConstructorUsedError;
  @JsonKey(name: 'is_head')
  bool get isHead => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $BranchInfoCopyWith<BranchInfo> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $BranchInfoCopyWith<$Res> {
  factory $BranchInfoCopyWith(
          BranchInfo value, $Res Function(BranchInfo) then) =
      _$BranchInfoCopyWithImpl<$Res, BranchInfo>;
  @useResult
  $Res call(
      {String name,
      @JsonKey(name: 'is_current') bool isCurrent,
      @JsonKey(name: 'is_head') bool isHead});
}

/// @nodoc
class _$BranchInfoCopyWithImpl<$Res, $Val extends BranchInfo>
    implements $BranchInfoCopyWith<$Res> {
  _$BranchInfoCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? name = null,
    Object? isCurrent = null,
    Object? isHead = null,
  }) {
    return _then(_value.copyWith(
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      isCurrent: null == isCurrent
          ? _value.isCurrent
          : isCurrent // ignore: cast_nullable_to_non_nullable
              as bool,
      isHead: null == isHead
          ? _value.isHead
          : isHead // ignore: cast_nullable_to_non_nullable
              as bool,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$BranchInfoImplCopyWith<$Res>
    implements $BranchInfoCopyWith<$Res> {
  factory _$$BranchInfoImplCopyWith(
          _$BranchInfoImpl value, $Res Function(_$BranchInfoImpl) then) =
      __$$BranchInfoImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String name,
      @JsonKey(name: 'is_current') bool isCurrent,
      @JsonKey(name: 'is_head') bool isHead});
}

/// @nodoc
class __$$BranchInfoImplCopyWithImpl<$Res>
    extends _$BranchInfoCopyWithImpl<$Res, _$BranchInfoImpl>
    implements _$$BranchInfoImplCopyWith<$Res> {
  __$$BranchInfoImplCopyWithImpl(
      _$BranchInfoImpl _value, $Res Function(_$BranchInfoImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? name = null,
    Object? isCurrent = null,
    Object? isHead = null,
  }) {
    return _then(_$BranchInfoImpl(
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      isCurrent: null == isCurrent
          ? _value.isCurrent
          : isCurrent // ignore: cast_nullable_to_non_nullable
              as bool,
      isHead: null == isHead
          ? _value.isHead
          : isHead // ignore: cast_nullable_to_non_nullable
              as bool,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$BranchInfoImpl implements _BranchInfo {
  const _$BranchInfoImpl(
      {required this.name,
      @JsonKey(name: 'is_current') this.isCurrent = false,
      @JsonKey(name: 'is_head') this.isHead = false});

  factory _$BranchInfoImpl.fromJson(Map<String, dynamic> json) =>
      _$$BranchInfoImplFromJson(json);

  @override
  final String name;
  @override
  @JsonKey(name: 'is_current')
  final bool isCurrent;
  @override
  @JsonKey(name: 'is_head')
  final bool isHead;

  @override
  String toString() {
    return 'BranchInfo(name: $name, isCurrent: $isCurrent, isHead: $isHead)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$BranchInfoImpl &&
            (identical(other.name, name) || other.name == name) &&
            (identical(other.isCurrent, isCurrent) ||
                other.isCurrent == isCurrent) &&
            (identical(other.isHead, isHead) || other.isHead == isHead));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, name, isCurrent, isHead);

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$BranchInfoImplCopyWith<_$BranchInfoImpl> get copyWith =>
      __$$BranchInfoImplCopyWithImpl<_$BranchInfoImpl>(this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$BranchInfoImplToJson(
      this,
    );
  }
}

abstract class _BranchInfo implements BranchInfo {
  const factory _BranchInfo(
      {required final String name,
      @JsonKey(name: 'is_current') final bool isCurrent,
      @JsonKey(name: 'is_head') final bool isHead}) = _$BranchInfoImpl;

  factory _BranchInfo.fromJson(Map<String, dynamic> json) =
      _$BranchInfoImpl.fromJson;

  @override
  String get name;
  @override
  @JsonKey(name: 'is_current')
  bool get isCurrent;
  @override
  @JsonKey(name: 'is_head')
  bool get isHead;
  @override
  @JsonKey(ignore: true)
  _$$BranchInfoImplCopyWith<_$BranchInfoImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

SkillFormField _$SkillFormFieldFromJson(Map<String, dynamic> json) {
  return _SkillFormField.fromJson(json);
}

/// @nodoc
mixin _$SkillFormField {
  String get name => throw _privateConstructorUsedError;
  String get label => throw _privateConstructorUsedError;
  String get type => throw _privateConstructorUsedError;
  bool get required => throw _privateConstructorUsedError;
  @JsonKey(name: 'default_value')
  String? get defaultValue => throw _privateConstructorUsedError;
  List<String> get options => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $SkillFormFieldCopyWith<SkillFormField> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $SkillFormFieldCopyWith<$Res> {
  factory $SkillFormFieldCopyWith(
          SkillFormField value, $Res Function(SkillFormField) then) =
      _$SkillFormFieldCopyWithImpl<$Res, SkillFormField>;
  @useResult
  $Res call(
      {String name,
      String label,
      String type,
      bool required,
      @JsonKey(name: 'default_value') String? defaultValue,
      List<String> options});
}

/// @nodoc
class _$SkillFormFieldCopyWithImpl<$Res, $Val extends SkillFormField>
    implements $SkillFormFieldCopyWith<$Res> {
  _$SkillFormFieldCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? name = null,
    Object? label = null,
    Object? type = null,
    Object? required = null,
    Object? defaultValue = freezed,
    Object? options = null,
  }) {
    return _then(_value.copyWith(
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      label: null == label
          ? _value.label
          : label // ignore: cast_nullable_to_non_nullable
              as String,
      type: null == type
          ? _value.type
          : type // ignore: cast_nullable_to_non_nullable
              as String,
      required: null == required
          ? _value.required
          : required // ignore: cast_nullable_to_non_nullable
              as bool,
      defaultValue: freezed == defaultValue
          ? _value.defaultValue
          : defaultValue // ignore: cast_nullable_to_non_nullable
              as String?,
      options: null == options
          ? _value.options
          : options // ignore: cast_nullable_to_non_nullable
              as List<String>,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$SkillFormFieldImplCopyWith<$Res>
    implements $SkillFormFieldCopyWith<$Res> {
  factory _$$SkillFormFieldImplCopyWith(_$SkillFormFieldImpl value,
          $Res Function(_$SkillFormFieldImpl) then) =
      __$$SkillFormFieldImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String name,
      String label,
      String type,
      bool required,
      @JsonKey(name: 'default_value') String? defaultValue,
      List<String> options});
}

/// @nodoc
class __$$SkillFormFieldImplCopyWithImpl<$Res>
    extends _$SkillFormFieldCopyWithImpl<$Res, _$SkillFormFieldImpl>
    implements _$$SkillFormFieldImplCopyWith<$Res> {
  __$$SkillFormFieldImplCopyWithImpl(
      _$SkillFormFieldImpl _value, $Res Function(_$SkillFormFieldImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? name = null,
    Object? label = null,
    Object? type = null,
    Object? required = null,
    Object? defaultValue = freezed,
    Object? options = null,
  }) {
    return _then(_$SkillFormFieldImpl(
      name: null == name
          ? _value.name
          : name // ignore: cast_nullable_to_non_nullable
              as String,
      label: null == label
          ? _value.label
          : label // ignore: cast_nullable_to_non_nullable
              as String,
      type: null == type
          ? _value.type
          : type // ignore: cast_nullable_to_non_nullable
              as String,
      required: null == required
          ? _value.required
          : required // ignore: cast_nullable_to_non_nullable
              as bool,
      defaultValue: freezed == defaultValue
          ? _value.defaultValue
          : defaultValue // ignore: cast_nullable_to_non_nullable
              as String?,
      options: null == options
          ? _value._options
          : options // ignore: cast_nullable_to_non_nullable
              as List<String>,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$SkillFormFieldImpl implements _SkillFormField {
  const _$SkillFormFieldImpl(
      {required this.name,
      required this.label,
      this.type = 'text',
      this.required = false,
      @JsonKey(name: 'default_value') this.defaultValue,
      final List<String> options = const []})
      : _options = options;

  factory _$SkillFormFieldImpl.fromJson(Map<String, dynamic> json) =>
      _$$SkillFormFieldImplFromJson(json);

  @override
  final String name;
  @override
  final String label;
  @override
  @JsonKey()
  final String type;
  @override
  @JsonKey()
  final bool required;
  @override
  @JsonKey(name: 'default_value')
  final String? defaultValue;
  final List<String> _options;
  @override
  @JsonKey()
  List<String> get options {
    if (_options is EqualUnmodifiableListView) return _options;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(_options);
  }

  @override
  String toString() {
    return 'SkillFormField(name: $name, label: $label, type: $type, required: $required, defaultValue: $defaultValue, options: $options)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$SkillFormFieldImpl &&
            (identical(other.name, name) || other.name == name) &&
            (identical(other.label, label) || other.label == label) &&
            (identical(other.type, type) || other.type == type) &&
            (identical(other.required, required) ||
                other.required == required) &&
            (identical(other.defaultValue, defaultValue) ||
                other.defaultValue == defaultValue) &&
            const DeepCollectionEquality().equals(other._options, _options));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, name, label, type, required,
      defaultValue, const DeepCollectionEquality().hash(_options));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$SkillFormFieldImplCopyWith<_$SkillFormFieldImpl> get copyWith =>
      __$$SkillFormFieldImplCopyWithImpl<_$SkillFormFieldImpl>(
          this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$SkillFormFieldImplToJson(
      this,
    );
  }
}

abstract class _SkillFormField implements SkillFormField {
  const factory _SkillFormField(
      {required final String name,
      required final String label,
      final String type,
      final bool required,
      @JsonKey(name: 'default_value') final String? defaultValue,
      final List<String> options}) = _$SkillFormFieldImpl;

  factory _SkillFormField.fromJson(Map<String, dynamic> json) =
      _$SkillFormFieldImpl.fromJson;

  @override
  String get name;
  @override
  String get label;
  @override
  String get type;
  @override
  bool get required;
  @override
  @JsonKey(name: 'default_value')
  String? get defaultValue;
  @override
  List<String> get options;
  @override
  @JsonKey(ignore: true)
  _$$SkillFormFieldImplCopyWith<_$SkillFormFieldImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

SkillUiDescriptor _$SkillUiDescriptorFromJson(Map<String, dynamic> json) {
  return _SkillUiDescriptor.fromJson(json);
}

/// @nodoc
mixin _$SkillUiDescriptor {
  @JsonKey(name: 'ui_type')
  String get uiType => throw _privateConstructorUsedError;
  @JsonKey(name: 'form_fields')
  List<SkillFormField> get formFields => throw _privateConstructorUsedError;
  List<String>? get actions => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $SkillUiDescriptorCopyWith<SkillUiDescriptor> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $SkillUiDescriptorCopyWith<$Res> {
  factory $SkillUiDescriptorCopyWith(
          SkillUiDescriptor value, $Res Function(SkillUiDescriptor) then) =
      _$SkillUiDescriptorCopyWithImpl<$Res, SkillUiDescriptor>;
  @useResult
  $Res call(
      {@JsonKey(name: 'ui_type') String uiType,
      @JsonKey(name: 'form_fields') List<SkillFormField> formFields,
      List<String>? actions});
}

/// @nodoc
class _$SkillUiDescriptorCopyWithImpl<$Res, $Val extends SkillUiDescriptor>
    implements $SkillUiDescriptorCopyWith<$Res> {
  _$SkillUiDescriptorCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? uiType = null,
    Object? formFields = null,
    Object? actions = freezed,
  }) {
    return _then(_value.copyWith(
      uiType: null == uiType
          ? _value.uiType
          : uiType // ignore: cast_nullable_to_non_nullable
              as String,
      formFields: null == formFields
          ? _value.formFields
          : formFields // ignore: cast_nullable_to_non_nullable
              as List<SkillFormField>,
      actions: freezed == actions
          ? _value.actions
          : actions // ignore: cast_nullable_to_non_nullable
              as List<String>?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$SkillUiDescriptorImplCopyWith<$Res>
    implements $SkillUiDescriptorCopyWith<$Res> {
  factory _$$SkillUiDescriptorImplCopyWith(_$SkillUiDescriptorImpl value,
          $Res Function(_$SkillUiDescriptorImpl) then) =
      __$$SkillUiDescriptorImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {@JsonKey(name: 'ui_type') String uiType,
      @JsonKey(name: 'form_fields') List<SkillFormField> formFields,
      List<String>? actions});
}

/// @nodoc
class __$$SkillUiDescriptorImplCopyWithImpl<$Res>
    extends _$SkillUiDescriptorCopyWithImpl<$Res, _$SkillUiDescriptorImpl>
    implements _$$SkillUiDescriptorImplCopyWith<$Res> {
  __$$SkillUiDescriptorImplCopyWithImpl(_$SkillUiDescriptorImpl _value,
      $Res Function(_$SkillUiDescriptorImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? uiType = null,
    Object? formFields = null,
    Object? actions = freezed,
  }) {
    return _then(_$SkillUiDescriptorImpl(
      uiType: null == uiType
          ? _value.uiType
          : uiType // ignore: cast_nullable_to_non_nullable
              as String,
      formFields: null == formFields
          ? _value._formFields
          : formFields // ignore: cast_nullable_to_non_nullable
              as List<SkillFormField>,
      actions: freezed == actions
          ? _value._actions
          : actions // ignore: cast_nullable_to_non_nullable
              as List<String>?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$SkillUiDescriptorImpl implements _SkillUiDescriptor {
  const _$SkillUiDescriptorImpl(
      {@JsonKey(name: 'ui_type') this.uiType = 'form',
      @JsonKey(name: 'form_fields')
      final List<SkillFormField> formFields = const [],
      final List<String>? actions})
      : _formFields = formFields,
        _actions = actions;

  factory _$SkillUiDescriptorImpl.fromJson(Map<String, dynamic> json) =>
      _$$SkillUiDescriptorImplFromJson(json);

  @override
  @JsonKey(name: 'ui_type')
  final String uiType;
  final List<SkillFormField> _formFields;
  @override
  @JsonKey(name: 'form_fields')
  List<SkillFormField> get formFields {
    if (_formFields is EqualUnmodifiableListView) return _formFields;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(_formFields);
  }

  final List<String>? _actions;
  @override
  List<String>? get actions {
    final value = _actions;
    if (value == null) return null;
    if (_actions is EqualUnmodifiableListView) return _actions;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(value);
  }

  @override
  String toString() {
    return 'SkillUiDescriptor(uiType: $uiType, formFields: $formFields, actions: $actions)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$SkillUiDescriptorImpl &&
            (identical(other.uiType, uiType) || other.uiType == uiType) &&
            const DeepCollectionEquality()
                .equals(other._formFields, _formFields) &&
            const DeepCollectionEquality().equals(other._actions, _actions));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(
      runtimeType,
      uiType,
      const DeepCollectionEquality().hash(_formFields),
      const DeepCollectionEquality().hash(_actions));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$SkillUiDescriptorImplCopyWith<_$SkillUiDescriptorImpl> get copyWith =>
      __$$SkillUiDescriptorImplCopyWithImpl<_$SkillUiDescriptorImpl>(
          this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$SkillUiDescriptorImplToJson(
      this,
    );
  }
}

abstract class _SkillUiDescriptor implements SkillUiDescriptor {
  const factory _SkillUiDescriptor(
      {@JsonKey(name: 'ui_type') final String uiType,
      @JsonKey(name: 'form_fields') final List<SkillFormField> formFields,
      final List<String>? actions}) = _$SkillUiDescriptorImpl;

  factory _SkillUiDescriptor.fromJson(Map<String, dynamic> json) =
      _$SkillUiDescriptorImpl.fromJson;

  @override
  @JsonKey(name: 'ui_type')
  String get uiType;
  @override
  @JsonKey(name: 'form_fields')
  List<SkillFormField> get formFields;
  @override
  List<String>? get actions;
  @override
  @JsonKey(ignore: true)
  _$$SkillUiDescriptorImplCopyWith<_$SkillUiDescriptorImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

SkillExecuteResult _$SkillExecuteResultFromJson(Map<String, dynamic> json) {
  return _SkillExecuteResult.fromJson(json);
}

/// @nodoc
mixin _$SkillExecuteResult {
  String get output => throw _privateConstructorUsedError;
  bool get success => throw _privateConstructorUsedError;
  String? get error => throw _privateConstructorUsedError;

  Map<String, dynamic> toJson() => throw _privateConstructorUsedError;
  @JsonKey(ignore: true)
  $SkillExecuteResultCopyWith<SkillExecuteResult> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $SkillExecuteResultCopyWith<$Res> {
  factory $SkillExecuteResultCopyWith(
          SkillExecuteResult value, $Res Function(SkillExecuteResult) then) =
      _$SkillExecuteResultCopyWithImpl<$Res, SkillExecuteResult>;
  @useResult
  $Res call({String output, bool success, String? error});
}

/// @nodoc
class _$SkillExecuteResultCopyWithImpl<$Res, $Val extends SkillExecuteResult>
    implements $SkillExecuteResultCopyWith<$Res> {
  _$SkillExecuteResultCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? output = null,
    Object? success = null,
    Object? error = freezed,
  }) {
    return _then(_value.copyWith(
      output: null == output
          ? _value.output
          : output // ignore: cast_nullable_to_non_nullable
              as String,
      success: null == success
          ? _value.success
          : success // ignore: cast_nullable_to_non_nullable
              as bool,
      error: freezed == error
          ? _value.error
          : error // ignore: cast_nullable_to_non_nullable
              as String?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$SkillExecuteResultImplCopyWith<$Res>
    implements $SkillExecuteResultCopyWith<$Res> {
  factory _$$SkillExecuteResultImplCopyWith(_$SkillExecuteResultImpl value,
          $Res Function(_$SkillExecuteResultImpl) then) =
      __$$SkillExecuteResultImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call({String output, bool success, String? error});
}

/// @nodoc
class __$$SkillExecuteResultImplCopyWithImpl<$Res>
    extends _$SkillExecuteResultCopyWithImpl<$Res, _$SkillExecuteResultImpl>
    implements _$$SkillExecuteResultImplCopyWith<$Res> {
  __$$SkillExecuteResultImplCopyWithImpl(_$SkillExecuteResultImpl _value,
      $Res Function(_$SkillExecuteResultImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? output = null,
    Object? success = null,
    Object? error = freezed,
  }) {
    return _then(_$SkillExecuteResultImpl(
      output: null == output
          ? _value.output
          : output // ignore: cast_nullable_to_non_nullable
              as String,
      success: null == success
          ? _value.success
          : success // ignore: cast_nullable_to_non_nullable
              as bool,
      error: freezed == error
          ? _value.error
          : error // ignore: cast_nullable_to_non_nullable
              as String?,
    ));
  }
}

/// @nodoc
@JsonSerializable()
class _$SkillExecuteResultImpl implements _SkillExecuteResult {
  const _$SkillExecuteResultImpl(
      {required this.output, required this.success, this.error});

  factory _$SkillExecuteResultImpl.fromJson(Map<String, dynamic> json) =>
      _$$SkillExecuteResultImplFromJson(json);

  @override
  final String output;
  @override
  final bool success;
  @override
  final String? error;

  @override
  String toString() {
    return 'SkillExecuteResult(output: $output, success: $success, error: $error)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$SkillExecuteResultImpl &&
            (identical(other.output, output) || other.output == output) &&
            (identical(other.success, success) || other.success == success) &&
            (identical(other.error, error) || other.error == error));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, output, success, error);

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$SkillExecuteResultImplCopyWith<_$SkillExecuteResultImpl> get copyWith =>
      __$$SkillExecuteResultImplCopyWithImpl<_$SkillExecuteResultImpl>(
          this, _$identity);

  @override
  Map<String, dynamic> toJson() {
    return _$$SkillExecuteResultImplToJson(
      this,
    );
  }
}

abstract class _SkillExecuteResult implements SkillExecuteResult {
  const factory _SkillExecuteResult(
      {required final String output,
      required final bool success,
      final String? error}) = _$SkillExecuteResultImpl;

  factory _SkillExecuteResult.fromJson(Map<String, dynamic> json) =
      _$SkillExecuteResultImpl.fromJson;

  @override
  String get output;
  @override
  bool get success;
  @override
  String? get error;
  @override
  @JsonKey(ignore: true)
  _$$SkillExecuteResultImplCopyWith<_$SkillExecuteResultImpl> get copyWith =>
      throw _privateConstructorUsedError;
}
