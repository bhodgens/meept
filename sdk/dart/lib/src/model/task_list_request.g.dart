// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'task_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TaskListRequest extends TaskListRequest {
  @override
  final int? limit;
  @override
  final String? sessionId;

  factory _$TaskListRequest([void Function(TaskListRequestBuilder)? updates]) =>
      (TaskListRequestBuilder()..update(updates))._build();

  _$TaskListRequest._({this.limit, this.sessionId})
      : super._();
  @override
  TaskListRequest rebuild(void Function(TaskListRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TaskListRequestBuilder toBuilder() => TaskListRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TaskListRequest &&
        limit == other.limit &&
        sessionId == other.sessionId;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jc(_$hash, sessionId.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TaskListRequest')
          ..add('limit', limit)
          ..add('sessionId', sessionId))
        .toString();
  }
}

class TaskListRequestBuilder
    implements Builder<TaskListRequest, TaskListRequestBuilder> {
  _$TaskListRequest? _$v;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  String? _sessionId;
  String? get sessionId => _$this._sessionId;
  set sessionId(String? sessionId) =>
      _$this._sessionId = sessionId;

  TaskListRequestBuilder() {
    TaskListRequest._defaults(this);
  }

  TaskListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limit = $v.limit;
      _sessionId = $v.sessionId;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TaskListRequest other) {
    _$v = other as _$TaskListRequest;
  }

  @override
  void update(void Function(TaskListRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TaskListRequest build() => _build();

  _$TaskListRequest _build() {
    final _$result = _$v ??
        _$TaskListRequest._(
          limit: limit,
          sessionId: sessionId,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
