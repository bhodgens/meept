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
  String? get sessionId => throw _privateConstructorUsedError;
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
      String? sessionId,
      List<String>? toolCalls});
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
      String? sessionId,
      List<String>? toolCalls});
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
class _$ChatMessageImpl implements _ChatMessage {
  const _$ChatMessageImpl(
      {required this.id,
      this.role = 'user',
      this.content = '',
      required this.timestamp,
      this.sessionId,
      final List<String>? toolCalls})
      : _toolCalls = toolCalls;

  factory _$ChatMessageImpl.fromJson(Map<String, dynamic> json) =>
      _$$ChatMessageImplFromJson(json);

  @override
  final String id;
  @override
  @JsonKey()
  final String role;
  @override
  @JsonKey()
  final String content;
  @override
  final DateTime timestamp;
  @override
  final String? sessionId;
  final List<String>? _toolCalls;
  @override
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

abstract class _ChatMessage implements ChatMessage {
  const factory _ChatMessage(
      {required final String id,
      final String role,
      final String content,
      required final DateTime timestamp,
      final String? sessionId,
      final List<String>? toolCalls}) = _$ChatMessageImpl;

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
  String? get sessionId;
  @override
  List<String>? get toolCalls;
  @override
  @JsonKey(ignore: true)
  _$$ChatMessageImplCopyWith<_$ChatMessageImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
mixin _$ChatRequest {
  String get message => throw _privateConstructorUsedError;
  String? get conversationId => throw _privateConstructorUsedError;
  String? get agentId => throw _privateConstructorUsedError;
  List<ChatMessage>? get history => throw _privateConstructorUsedError;

  @JsonKey(ignore: true)
  $ChatRequestCopyWith<ChatRequest> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
abstract class $ChatRequestCopyWith<$Res> {
  factory $ChatRequestCopyWith(
          ChatRequest value, $Res Function(ChatRequest) then) =
      _$ChatRequestCopyWithImpl<$Res, ChatRequest>;
  @useResult
  $Res call(
      {String message,
      String? conversationId,
      String? agentId,
      List<ChatMessage>? history});
}

/// @nodoc
class _$ChatRequestCopyWithImpl<$Res, $Val extends ChatRequest>
    implements $ChatRequestCopyWith<$Res> {
  _$ChatRequestCopyWithImpl(this._value, this._then);

  // ignore: unused_field
  final $Val _value;
  // ignore: unused_field
  final $Res Function($Val) _then;

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? message = null,
    Object? conversationId = freezed,
    Object? agentId = freezed,
    Object? history = freezed,
  }) {
    return _then(_value.copyWith(
      message: null == message
          ? _value.message
          : message // ignore: cast_nullable_to_non_nullable
              as String,
      conversationId: freezed == conversationId
          ? _value.conversationId
          : conversationId // ignore: cast_nullable_to_non_nullable
              as String?,
      agentId: freezed == agentId
          ? _value.agentId
          : agentId // ignore: cast_nullable_to_non_nullable
              as String?,
      history: freezed == history
          ? _value.history
          : history // ignore: cast_nullable_to_non_nullable
              as List<ChatMessage>?,
    ) as $Val);
  }
}

/// @nodoc
abstract class _$$ChatRequestImplCopyWith<$Res>
    implements $ChatRequestCopyWith<$Res> {
  factory _$$ChatRequestImplCopyWith(
          _$ChatRequestImpl value, $Res Function(_$ChatRequestImpl) then) =
      __$$ChatRequestImplCopyWithImpl<$Res>;
  @override
  @useResult
  $Res call(
      {String message,
      String? conversationId,
      String? agentId,
      List<ChatMessage>? history});
}

/// @nodoc
class __$$ChatRequestImplCopyWithImpl<$Res>
    extends _$ChatRequestCopyWithImpl<$Res, _$ChatRequestImpl>
    implements _$$ChatRequestImplCopyWith<$Res> {
  __$$ChatRequestImplCopyWithImpl(
      _$ChatRequestImpl _value, $Res Function(_$ChatRequestImpl) _then)
      : super(_value, _then);

  @pragma('vm:prefer-inline')
  @override
  $Res call({
    Object? message = null,
    Object? conversationId = freezed,
    Object? agentId = freezed,
    Object? history = freezed,
  }) {
    return _then(_$ChatRequestImpl(
      message: null == message
          ? _value.message
          : message // ignore: cast_nullable_to_non_nullable
              as String,
      conversationId: freezed == conversationId
          ? _value.conversationId
          : conversationId // ignore: cast_nullable_to_non_nullable
              as String?,
      agentId: freezed == agentId
          ? _value.agentId
          : agentId // ignore: cast_nullable_to_non_nullable
              as String?,
      history: freezed == history
          ? _value._history
          : history // ignore: cast_nullable_to_non_nullable
              as List<ChatMessage>?,
    ));
  }
}

/// @nodoc

class _$ChatRequestImpl implements _ChatRequest {
  const _$ChatRequestImpl(
      {required this.message,
      this.conversationId,
      this.agentId,
      final List<ChatMessage>? history})
      : _history = history;

  @override
  final String message;
  @override
  final String? conversationId;
  @override
  final String? agentId;
  final List<ChatMessage>? _history;
  @override
  List<ChatMessage>? get history {
    final value = _history;
    if (value == null) return null;
    if (_history is EqualUnmodifiableListView) return _history;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableListView(value);
  }

  @override
  String toString() {
    return 'ChatRequest(message: $message, conversationId: $conversationId, agentId: $agentId, history: $history)';
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is _$ChatRequestImpl &&
            (identical(other.message, message) || other.message == message) &&
            (identical(other.conversationId, conversationId) ||
                other.conversationId == conversationId) &&
            (identical(other.agentId, agentId) || other.agentId == agentId) &&
            const DeepCollectionEquality().equals(other._history, _history));
  }

  @override
  int get hashCode => Object.hash(runtimeType, message, conversationId, agentId,
      const DeepCollectionEquality().hash(_history));

  @JsonKey(ignore: true)
  @override
  @pragma('vm:prefer-inline')
  _$$ChatRequestImplCopyWith<_$ChatRequestImpl> get copyWith =>
      __$$ChatRequestImplCopyWithImpl<_$ChatRequestImpl>(this, _$identity);
}

abstract class _ChatRequest implements ChatRequest {
  const factory _ChatRequest(
      {required final String message,
      final String? conversationId,
      final String? agentId,
      final List<ChatMessage>? history}) = _$ChatRequestImpl;

  @override
  String get message;
  @override
  String? get conversationId;
  @override
  String? get agentId;
  @override
  List<ChatMessage>? get history;
  @override
  @JsonKey(ignore: true)
  _$$ChatRequestImplCopyWith<_$ChatRequestImpl> get copyWith =>
      throw _privateConstructorUsedError;
}

/// @nodoc
mixin _$Session {
  String get id => throw _privateConstructorUsedError;
  String get title => throw _privateConstructorUsedError;
  String? get description => throw _privateConstructorUsedError;
  String? get conversationId => throw _privateConstructorUsedError;
  DateTime get createdAt => throw _privateConstructorUsedError;
  DateTime? get lastActivity => throw _privateConstructorUsedError;
  List<String>? get attachedClients => throw _privateConstructorUsedError;

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
      String title,
      String? description,
      String? conversationId,
      DateTime createdAt,
      DateTime? lastActivity,
      List<String>? attachedClients});
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
      String title,
      String? description,
      String? conversationId,
      DateTime createdAt,
      DateTime? lastActivity,
      List<String>? attachedClients});
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

class _$SessionImpl implements _Session {
  const _$SessionImpl(
      {required this.id,
      required this.title,
      this.description,
      this.conversationId,
      required this.createdAt,
      this.lastActivity,
      final List<String>? attachedClients})
      : _attachedClients = attachedClients;

  @override
  final String id;
  @override
  final String title;
  @override
  final String? description;
  @override
  final String? conversationId;
  @override
  final DateTime createdAt;
  @override
  final DateTime? lastActivity;
  final List<String>? _attachedClients;
  @override
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
}

abstract class _Session implements Session {
  const factory _Session(
      {required final String id,
      required final String title,
      final String? description,
      final String? conversationId,
      required final DateTime createdAt,
      final DateTime? lastActivity,
      final List<String>? attachedClients}) = _$SessionImpl;

  @override
  String get id;
  @override
  String get title;
  @override
  String? get description;
  @override
  String? get conversationId;
  @override
  DateTime get createdAt;
  @override
  DateTime? get lastActivity;
  @override
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
  String get title => throw _privateConstructorUsedError;
  String get description => throw _privateConstructorUsedError;
  String get status => throw _privateConstructorUsedError;
  String? get agentId => throw _privateConstructorUsedError;
  String? get sessionId => throw _privateConstructorUsedError;
  DateTime get createdAt => throw _privateConstructorUsedError;
  DateTime? get updatedAt => throw _privateConstructorUsedError;
  DateTime? get completedAt => throw _privateConstructorUsedError;
  Map<String, dynamic>? get metadata => throw _privateConstructorUsedError;
  int? get totalJobs => throw _privateConstructorUsedError;
  int? get completedJobs => throw _privateConstructorUsedError;
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
      String title,
      String description,
      String status,
      String? agentId,
      String? sessionId,
      DateTime createdAt,
      DateTime? updatedAt,
      DateTime? completedAt,
      Map<String, dynamic>? metadata,
      int? totalJobs,
      int? completedJobs,
      int? failedJobs,
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
      String title,
      String description,
      String status,
      String? agentId,
      String? sessionId,
      DateTime createdAt,
      DateTime? updatedAt,
      DateTime? completedAt,
      Map<String, dynamic>? metadata,
      int? totalJobs,
      int? completedJobs,
      int? failedJobs,
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
class _$TaskImpl implements _Task {
  const _$TaskImpl(
      {required this.id,
      this.title = '',
      this.description = '',
      this.status = 'pending',
      this.agentId,
      this.sessionId,
      required this.createdAt,
      this.updatedAt,
      this.completedAt,
      final Map<String, dynamic>? metadata,
      this.totalJobs,
      this.completedJobs,
      this.failedJobs,
      final List<TaskStep>? steps})
      : _metadata = metadata,
        _steps = steps;

  factory _$TaskImpl.fromJson(Map<String, dynamic> json) =>
      _$$TaskImplFromJson(json);

  @override
  final String id;
  @override
  @JsonKey()
  final String title;
  @override
  @JsonKey()
  final String description;
  @override
  @JsonKey()
  final String status;
  @override
  final String? agentId;
  @override
  final String? sessionId;
  @override
  final DateTime createdAt;
  @override
  final DateTime? updatedAt;
  @override
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
  final int? totalJobs;
  @override
  final int? completedJobs;
  @override
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

abstract class _Task implements Task {
  const factory _Task(
      {required final String id,
      final String title,
      final String description,
      final String status,
      final String? agentId,
      final String? sessionId,
      required final DateTime createdAt,
      final DateTime? updatedAt,
      final DateTime? completedAt,
      final Map<String, dynamic>? metadata,
      final int? totalJobs,
      final int? completedJobs,
      final int? failedJobs,
      final List<TaskStep>? steps}) = _$TaskImpl;

  factory _Task.fromJson(Map<String, dynamic> json) = _$TaskImpl.fromJson;

  @override
  String get id;
  @override
  String get title;
  @override
  String get description;
  @override
  String get status;
  @override
  String? get agentId;
  @override
  String? get sessionId;
  @override
  DateTime get createdAt;
  @override
  DateTime? get updatedAt;
  @override
  DateTime? get completedAt;
  @override
  Map<String, dynamic>? get metadata;
  @override
  int? get totalJobs;
  @override
  int? get completedJobs;
  @override
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
  String get taskId => throw _privateConstructorUsedError;
  String get description => throw _privateConstructorUsedError;
  String get status => throw _privateConstructorUsedError;
  String? get output => throw _privateConstructorUsedError;
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
      String taskId,
      String description,
      String status,
      String? output,
      DateTime? completedAt});
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
      String taskId,
      String description,
      String status,
      String? output,
      DateTime? completedAt});
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
      required this.taskId,
      required this.description,
      this.status = 'pending',
      this.output,
      this.completedAt});

  factory _$TaskStepImpl.fromJson(Map<String, dynamic> json) =>
      _$$TaskStepImplFromJson(json);

  @override
  final String id;
  @override
  final String taskId;
  @override
  final String description;
  @override
  @JsonKey()
  final String status;
  @override
  final String? output;
  @override
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
      required final String taskId,
      required final String description,
      final String status,
      final String? output,
      final DateTime? completedAt}) = _$TaskStepImpl;

  factory _TaskStep.fromJson(Map<String, dynamic> json) =
      _$TaskStepImpl.fromJson;

  @override
  String get id;
  @override
  String get taskId;
  @override
  String get description;
  @override
  String get status;
  @override
  String? get output;
  @override
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
  String get status => throw _privateConstructorUsedError;
  String? get agentId => throw _privateConstructorUsedError;
  Map<String, dynamic> get payload => throw _privateConstructorUsedError;
  DateTime get createdAt => throw _privateConstructorUsedError;
  DateTime? get completedAt => throw _privateConstructorUsedError;
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
      String status,
      String? agentId,
      Map<String, dynamic> payload,
      DateTime createdAt,
      DateTime? completedAt,
      int retryCount,
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
    Object? payload = null,
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
      payload: null == payload
          ? _value.payload
          : payload // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>,
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
      String status,
      String? agentId,
      Map<String, dynamic> payload,
      DateTime createdAt,
      DateTime? completedAt,
      int retryCount,
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
    Object? payload = null,
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
      payload: null == payload
          ? _value._payload
          : payload // ignore: cast_nullable_to_non_nullable
              as Map<String, dynamic>,
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
      this.status = 'pending',
      this.agentId,
      final Map<String, dynamic> payload = const {},
      required this.createdAt,
      this.completedAt,
      this.retryCount = 0,
      this.error})
      : _payload = payload;

  factory _$JobImpl.fromJson(Map<String, dynamic> json) =>
      _$$JobImplFromJson(json);

  @override
  final String id;
  @override
  final String type;
  @override
  @JsonKey()
  final String status;
  @override
  final String? agentId;
  final Map<String, dynamic> _payload;
  @override
  @JsonKey()
  Map<String, dynamic> get payload {
    if (_payload is EqualUnmodifiableMapView) return _payload;
    // ignore: implicit_dynamic_type
    return EqualUnmodifiableMapView(_payload);
  }

  @override
  final DateTime createdAt;
  @override
  final DateTime? completedAt;
  @override
  @JsonKey()
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
      final String status,
      final String? agentId,
      final Map<String, dynamic> payload,
      required final DateTime createdAt,
      final DateTime? completedAt,
      final int retryCount,
      final String? error}) = _$JobImpl;

  factory _Job.fromJson(Map<String, dynamic> json) = _$JobImpl.fromJson;

  @override
  String get id;
  @override
  String get type;
  @override
  String get status;
  @override
  String? get agentId;
  @override
  Map<String, dynamic> get payload;
  @override
  DateTime get createdAt;
  @override
  DateTime? get completedAt;
  @override
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
      {this.slug = '',
      this.name = '',
      this.description = '',
      this.category = '',
      final List<String> capabilities = const [],
      this.enabled = true})
      : _capabilities = capabilities;

  factory _$SkillImpl.fromJson(Map<String, dynamic> json) =>
      _$$SkillImplFromJson(json);

  @override
  @JsonKey()
  final String slug;
  @override
  @JsonKey()
  final String name;
  @override
  @JsonKey()
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

  @override
  @JsonKey()
  final bool enabled;

  @override
  String toString() {
    return 'Skill(slug: $slug, name: $name, description: $description, category: $category, capabilities: $capabilities, enabled: $enabled)';
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
            (identical(other.enabled, enabled) || other.enabled == enabled));
  }

  @JsonKey(ignore: true)
  @override
  int get hashCode => Object.hash(runtimeType, slug, name, description,
      category, const DeepCollectionEquality().hash(_capabilities), enabled);

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
      {final String slug,
      final String name,
      final String description,
      final String category,
      final List<String> capabilities,
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
  int get activeAgents => throw _privateConstructorUsedError;
  double get requestsPerSec => throw _privateConstructorUsedError;
  double get tokenUsageRate => throw _privateConstructorUsedError;
  int get queueDepth => throw _privateConstructorUsedError;
  int get totalSessions => throw _privateConstructorUsedError;
  int get totalJobs => throw _privateConstructorUsedError;
  int get runningJobs => throw _privateConstructorUsedError;
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
      int activeAgents,
      double requestsPerSec,
      double tokenUsageRate,
      int queueDepth,
      int totalSessions,
      int totalJobs,
      int runningJobs,
      int pendingJobs,
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
      int activeAgents,
      double requestsPerSec,
      double tokenUsageRate,
      int queueDepth,
      int totalSessions,
      int totalJobs,
      int runningJobs,
      int pendingJobs,
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
      this.activeAgents = 0,
      this.requestsPerSec = 0.0,
      this.tokenUsageRate = 0.0,
      this.queueDepth = 0,
      this.totalSessions = 0,
      this.totalJobs = 0,
      this.runningJobs = 0,
      this.pendingJobs = 0,
      this.version = '',
      final Map<String, dynamic>? metadata})
      : _metadata = metadata;

  factory _$MetricsSnapshotImpl.fromJson(Map<String, dynamic> json) =>
      _$$MetricsSnapshotImplFromJson(json);

  @override
  final DateTime timestamp;
  @override
  @JsonKey()
  final int activeAgents;
  @override
  @JsonKey()
  final double requestsPerSec;
  @override
  @JsonKey()
  final double tokenUsageRate;
  @override
  @JsonKey()
  final int queueDepth;
  @override
  @JsonKey()
  final int totalSessions;
  @override
  @JsonKey()
  final int totalJobs;
  @override
  @JsonKey()
  final int runningJobs;
  @override
  @JsonKey()
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
      final int activeAgents,
      final double requestsPerSec,
      final double tokenUsageRate,
      final int queueDepth,
      final int totalSessions,
      final int totalJobs,
      final int runningJobs,
      final int pendingJobs,
      final String version,
      final Map<String, dynamic>? metadata}) = _$MetricsSnapshotImpl;

  factory _MetricsSnapshot.fromJson(Map<String, dynamic> json) =
      _$MetricsSnapshotImpl.fromJson;

  @override
  DateTime get timestamp;
  @override
  int get activeAgents;
  @override
  double get requestsPerSec;
  @override
  double get tokenUsageRate;
  @override
  int get queueDepth;
  @override
  int get totalSessions;
  @override
  int get totalJobs;
  @override
  int get runningJobs;
  @override
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
  String get filePath => throw _privateConstructorUsedError;
  String? get projectID => throw _privateConstructorUsedError;
  String get state => throw _privateConstructorUsedError;
  DateTime get createdAt => throw _privateConstructorUsedError;
  DateTime get updatedAt => throw _privateConstructorUsedError;
  DateTime? get approvedAt => throw _privateConstructorUsedError;
  DateTime? get confirmedAt => throw _privateConstructorUsedError;
  String? get approvedBy => throw _privateConstructorUsedError;
  String? get confirmedBy => throw _privateConstructorUsedError;
  String? get taskID => throw _privateConstructorUsedError;
  String? get sourceSession => throw _privateConstructorUsedError;
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
      String filePath,
      String? projectID,
      String state,
      DateTime createdAt,
      DateTime updatedAt,
      DateTime? approvedAt,
      DateTime? confirmedAt,
      String? approvedBy,
      String? confirmedBy,
      String? taskID,
      String? sourceSession,
      int revisionCount,
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
      String filePath,
      String? projectID,
      String state,
      DateTime createdAt,
      DateTime updatedAt,
      DateTime? approvedAt,
      DateTime? confirmedAt,
      String? approvedBy,
      String? confirmedBy,
      String? taskID,
      String? sourceSession,
      int revisionCount,
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
      this.filePath = '',
      this.projectID,
      required this.state,
      required this.createdAt,
      required this.updatedAt,
      this.approvedAt,
      this.confirmedAt,
      this.approvedBy,
      this.confirmedBy,
      this.taskID,
      this.sourceSession,
      this.revisionCount = 0,
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
  @JsonKey()
  final String filePath;
  @override
  final String? projectID;
  @override
  final String state;
  @override
  final DateTime createdAt;
  @override
  final DateTime updatedAt;
  @override
  final DateTime? approvedAt;
  @override
  final DateTime? confirmedAt;
  @override
  final String? approvedBy;
  @override
  final String? confirmedBy;
  @override
  final String? taskID;
  @override
  final String? sourceSession;
  @override
  @JsonKey()
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
      final String filePath,
      final String? projectID,
      required final String state,
      required final DateTime createdAt,
      required final DateTime updatedAt,
      final DateTime? approvedAt,
      final DateTime? confirmedAt,
      final String? approvedBy,
      final String? confirmedBy,
      final String? taskID,
      final String? sourceSession,
      final int revisionCount,
      final List<PlanPhase> phases}) = _$PlanImpl;

  factory _Plan.fromJson(Map<String, dynamic> json) = _$PlanImpl.fromJson;

  @override
  String get id;
  @override
  String get title;
  @override
  String get description;
  @override
  String get filePath;
  @override
  String? get projectID;
  @override
  String get state;
  @override
  DateTime get createdAt;
  @override
  DateTime get updatedAt;
  @override
  DateTime? get approvedAt;
  @override
  DateTime? get confirmedAt;
  @override
  String? get approvedBy;
  @override
  String? get confirmedBy;
  @override
  String? get taskID;
  @override
  String? get sourceSession;
  @override
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
  String get planID => throw _privateConstructorUsedError;
  String get name => throw _privateConstructorUsedError;
  int get sequence => throw _privateConstructorUsedError;
  int get totalSteps => throw _privateConstructorUsedError;
  int get completedSteps => throw _privateConstructorUsedError;
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
      String planID,
      String name,
      int sequence,
      int totalSteps,
      int completedSteps,
      int failedSteps,
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
      String planID,
      String name,
      int sequence,
      int totalSteps,
      int completedSteps,
      int failedSteps,
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
      required this.planID,
      required this.name,
      this.sequence = 0,
      this.totalSteps = 0,
      this.completedSteps = 0,
      this.failedSteps = 0,
      required this.state});

  factory _$PlanPhaseImpl.fromJson(Map<String, dynamic> json) =>
      _$$PlanPhaseImplFromJson(json);

  @override
  final String id;
  @override
  final String planID;
  @override
  final String name;
  @override
  @JsonKey()
  final int sequence;
  @override
  @JsonKey()
  final int totalSteps;
  @override
  @JsonKey()
  final int completedSteps;
  @override
  @JsonKey()
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
      required final String planID,
      required final String name,
      final int sequence,
      final int totalSteps,
      final int completedSteps,
      final int failedSteps,
      required final String state}) = _$PlanPhaseImpl;

  factory _PlanPhase.fromJson(Map<String, dynamic> json) =
      _$PlanPhaseImpl.fromJson;

  @override
  String get id;
  @override
  String get planID;
  @override
  String get name;
  @override
  int get sequence;
  @override
  int get totalSteps;
  @override
  int get completedSteps;
  @override
  int get failedSteps;
  @override
  String get state;
  @override
  @JsonKey(ignore: true)
  _$$PlanPhaseImplCopyWith<_$PlanPhaseImpl> get copyWith =>
      throw _privateConstructorUsedError;
}
