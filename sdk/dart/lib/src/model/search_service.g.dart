// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'search_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SearchService extends SearchService {
  @override
  final JsonObject? sessionStore;
  @override
  final JsonObject? taskRegistry;
  @override
  final JsonObject? memoryMgr;
  @override
  final JsonObject? planStore;

  factory _$SearchService([void Function(SearchServiceBuilder)? updates]) =>
      (SearchServiceBuilder()..update(updates))._build();

  _$SearchService._(
      {this.sessionStore, this.taskRegistry, this.memoryMgr, this.planStore})
      : super._();
  @override
  SearchService rebuild(void Function(SearchServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SearchServiceBuilder toBuilder() => SearchServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SearchService &&
        sessionStore == other.sessionStore &&
        taskRegistry == other.taskRegistry &&
        memoryMgr == other.memoryMgr &&
        planStore == other.planStore;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, sessionStore.hashCode);
    _$hash = $jc(_$hash, taskRegistry.hashCode);
    _$hash = $jc(_$hash, memoryMgr.hashCode);
    _$hash = $jc(_$hash, planStore.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SearchService')
          ..add('sessionStore', sessionStore)
          ..add('taskRegistry', taskRegistry)
          ..add('memoryMgr', memoryMgr)
          ..add('planStore', planStore))
        .toString();
  }
}

class SearchServiceBuilder
    implements Builder<SearchService, SearchServiceBuilder> {
  _$SearchService? _$v;

  JsonObject? _sessionStore;
  JsonObject? get sessionStore => _$this._sessionStore;
  set sessionStore(JsonObject? sessionStore) =>
      _$this._sessionStore = sessionStore;

  JsonObject? _taskRegistry;
  JsonObject? get taskRegistry => _$this._taskRegistry;
  set taskRegistry(JsonObject? taskRegistry) =>
      _$this._taskRegistry = taskRegistry;

  JsonObject? _memoryMgr;
  JsonObject? get memoryMgr => _$this._memoryMgr;
  set memoryMgr(JsonObject? memoryMgr) => _$this._memoryMgr = memoryMgr;

  JsonObject? _planStore;
  JsonObject? get planStore => _$this._planStore;
  set planStore(JsonObject? planStore) => _$this._planStore = planStore;

  SearchServiceBuilder() {
    SearchService._defaults(this);
  }

  SearchServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _sessionStore = $v.sessionStore;
      _taskRegistry = $v.taskRegistry;
      _memoryMgr = $v.memoryMgr;
      _planStore = $v.planStore;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SearchService other) {
    _$v = other as _$SearchService;
  }

  @override
  void update(void Function(SearchServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SearchService build() => _build();

  _$SearchService _build() {
    final _$result = _$v ??
        _$SearchService._(
          sessionStore: sessionStore,
          taskRegistry: taskRegistry,
          memoryMgr: memoryMgr,
          planStore: planStore,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
