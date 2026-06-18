// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'service_registry.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ServiceRegistry extends ServiceRegistry {
  @override
  final JsonObject? chat;
  @override
  final JsonObject? memory;
  @override
  final JsonObject? task;
  @override
  final JsonObject? queue;
  @override
  final JsonObject? session;
  @override
  final JsonObject? sessionStore;
  @override
  final JsonObject? worker;
  @override
  final JsonObject? pipeline;
  @override
  final JsonObject? skills;
  @override
  final JsonObject? selfImprove;
  @override
  final JsonObject? cache;
  @override
  final JsonObject? security;
  @override
  final JsonObject? scheduler;
  @override
  final JsonObject? bus;
  @override
  final JsonObject? templates;
  @override
  final JsonObject? daemon;
  @override
  final JsonObject? model;
  @override
  final JsonObject? calendar;
  @override
  final JsonObject? runtime;
  @override
  final JsonObject? terminal;
  @override
  final JsonObject? project;
  @override
  final JsonObject? plan;
  @override
  final JsonObject? search;

  factory _$ServiceRegistry([void Function(ServiceRegistryBuilder)? updates]) =>
      (ServiceRegistryBuilder()..update(updates))._build();

  _$ServiceRegistry._(
      {this.chat,
      this.memory,
      this.task,
      this.queue,
      this.session,
      this.sessionStore,
      this.worker,
      this.pipeline,
      this.skills,
      this.selfImprove,
      this.cache,
      this.security,
      this.scheduler,
      this.bus,
      this.templates,
      this.daemon,
      this.model,
      this.calendar,
      this.runtime,
      this.terminal,
      this.project,
      this.plan,
      this.search})
      : super._();
  @override
  ServiceRegistry rebuild(void Function(ServiceRegistryBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ServiceRegistryBuilder toBuilder() => ServiceRegistryBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ServiceRegistry &&
        chat == other.chat &&
        memory == other.memory &&
        task == other.task &&
        queue == other.queue &&
        session == other.session &&
        sessionStore == other.sessionStore &&
        worker == other.worker &&
        pipeline == other.pipeline &&
        skills == other.skills &&
        selfImprove == other.selfImprove &&
        cache == other.cache &&
        security == other.security &&
        scheduler == other.scheduler &&
        bus == other.bus &&
        templates == other.templates &&
        daemon == other.daemon &&
        model == other.model &&
        calendar == other.calendar &&
        runtime == other.runtime &&
        terminal == other.terminal &&
        project == other.project &&
        plan == other.plan &&
        search == other.search;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, chat.hashCode);
    _$hash = $jc(_$hash, memory.hashCode);
    _$hash = $jc(_$hash, task.hashCode);
    _$hash = $jc(_$hash, queue.hashCode);
    _$hash = $jc(_$hash, session.hashCode);
    _$hash = $jc(_$hash, sessionStore.hashCode);
    _$hash = $jc(_$hash, worker.hashCode);
    _$hash = $jc(_$hash, pipeline.hashCode);
    _$hash = $jc(_$hash, skills.hashCode);
    _$hash = $jc(_$hash, selfImprove.hashCode);
    _$hash = $jc(_$hash, cache.hashCode);
    _$hash = $jc(_$hash, security.hashCode);
    _$hash = $jc(_$hash, scheduler.hashCode);
    _$hash = $jc(_$hash, bus.hashCode);
    _$hash = $jc(_$hash, templates.hashCode);
    _$hash = $jc(_$hash, daemon.hashCode);
    _$hash = $jc(_$hash, model.hashCode);
    _$hash = $jc(_$hash, calendar.hashCode);
    _$hash = $jc(_$hash, runtime.hashCode);
    _$hash = $jc(_$hash, terminal.hashCode);
    _$hash = $jc(_$hash, project.hashCode);
    _$hash = $jc(_$hash, plan.hashCode);
    _$hash = $jc(_$hash, search.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ServiceRegistry')
          ..add('chat', chat)
          ..add('memory', memory)
          ..add('task', task)
          ..add('queue', queue)
          ..add('session', session)
          ..add('sessionStore', sessionStore)
          ..add('worker', worker)
          ..add('pipeline', pipeline)
          ..add('skills', skills)
          ..add('selfImprove', selfImprove)
          ..add('cache', cache)
          ..add('security', security)
          ..add('scheduler', scheduler)
          ..add('bus', bus)
          ..add('templates', templates)
          ..add('daemon', daemon)
          ..add('model', model)
          ..add('calendar', calendar)
          ..add('runtime', runtime)
          ..add('terminal', terminal)
          ..add('project', project)
          ..add('plan', plan)
          ..add('search', search))
        .toString();
  }
}

class ServiceRegistryBuilder
    implements Builder<ServiceRegistry, ServiceRegistryBuilder> {
  _$ServiceRegistry? _$v;

  JsonObject? _chat;
  JsonObject? get chat => _$this._chat;
  set chat(JsonObject? chat) => _$this._chat = chat;

  JsonObject? _memory;
  JsonObject? get memory => _$this._memory;
  set memory(JsonObject? memory) => _$this._memory = memory;

  JsonObject? _task;
  JsonObject? get task => _$this._task;
  set task(JsonObject? task) => _$this._task = task;

  JsonObject? _queue;
  JsonObject? get queue => _$this._queue;
  set queue(JsonObject? queue) => _$this._queue = queue;

  JsonObject? _session;
  JsonObject? get session => _$this._session;
  set session(JsonObject? session) => _$this._session = session;

  JsonObject? _sessionStore;
  JsonObject? get sessionStore => _$this._sessionStore;
  set sessionStore(JsonObject? sessionStore) =>
      _$this._sessionStore = sessionStore;

  JsonObject? _worker;
  JsonObject? get worker => _$this._worker;
  set worker(JsonObject? worker) => _$this._worker = worker;

  JsonObject? _pipeline;
  JsonObject? get pipeline => _$this._pipeline;
  set pipeline(JsonObject? pipeline) => _$this._pipeline = pipeline;

  JsonObject? _skills;
  JsonObject? get skills => _$this._skills;
  set skills(JsonObject? skills) => _$this._skills = skills;

  JsonObject? _selfImprove;
  JsonObject? get selfImprove => _$this._selfImprove;
  set selfImprove(JsonObject? selfImprove) => _$this._selfImprove = selfImprove;

  JsonObject? _cache;
  JsonObject? get cache => _$this._cache;
  set cache(JsonObject? cache) => _$this._cache = cache;

  JsonObject? _security;
  JsonObject? get security => _$this._security;
  set security(JsonObject? security) => _$this._security = security;

  JsonObject? _scheduler;
  JsonObject? get scheduler => _$this._scheduler;
  set scheduler(JsonObject? scheduler) => _$this._scheduler = scheduler;

  JsonObject? _bus;
  JsonObject? get bus => _$this._bus;
  set bus(JsonObject? bus) => _$this._bus = bus;

  JsonObject? _templates;
  JsonObject? get templates => _$this._templates;
  set templates(JsonObject? templates) => _$this._templates = templates;

  JsonObject? _daemon;
  JsonObject? get daemon => _$this._daemon;
  set daemon(JsonObject? daemon) => _$this._daemon = daemon;

  JsonObject? _model;
  JsonObject? get model => _$this._model;
  set model(JsonObject? model) => _$this._model = model;

  JsonObject? _calendar;
  JsonObject? get calendar => _$this._calendar;
  set calendar(JsonObject? calendar) => _$this._calendar = calendar;

  JsonObject? _runtime;
  JsonObject? get runtime => _$this._runtime;
  set runtime(JsonObject? runtime) => _$this._runtime = runtime;

  JsonObject? _terminal;
  JsonObject? get terminal => _$this._terminal;
  set terminal(JsonObject? terminal) => _$this._terminal = terminal;

  JsonObject? _project;
  JsonObject? get project => _$this._project;
  set project(JsonObject? project) => _$this._project = project;

  JsonObject? _plan;
  JsonObject? get plan => _$this._plan;
  set plan(JsonObject? plan) => _$this._plan = plan;

  JsonObject? _search;
  JsonObject? get search => _$this._search;
  set search(JsonObject? search) => _$this._search = search;

  ServiceRegistryBuilder() {
    ServiceRegistry._defaults(this);
  }

  ServiceRegistryBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _chat = $v.chat;
      _memory = $v.memory;
      _task = $v.task;
      _queue = $v.queue;
      _session = $v.session;
      _sessionStore = $v.sessionStore;
      _worker = $v.worker;
      _pipeline = $v.pipeline;
      _skills = $v.skills;
      _selfImprove = $v.selfImprove;
      _cache = $v.cache;
      _security = $v.security;
      _scheduler = $v.scheduler;
      _bus = $v.bus;
      _templates = $v.templates;
      _daemon = $v.daemon;
      _model = $v.model;
      _calendar = $v.calendar;
      _runtime = $v.runtime;
      _terminal = $v.terminal;
      _project = $v.project;
      _plan = $v.plan;
      _search = $v.search;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ServiceRegistry other) {
    _$v = other as _$ServiceRegistry;
  }

  @override
  void update(void Function(ServiceRegistryBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ServiceRegistry build() => _build();

  _$ServiceRegistry _build() {
    final _$result = _$v ??
        _$ServiceRegistry._(
          chat: chat,
          memory: memory,
          task: task,
          queue: queue,
          session: session,
          sessionStore: sessionStore,
          worker: worker,
          pipeline: pipeline,
          skills: skills,
          selfImprove: selfImprove,
          cache: cache,
          security: security,
          scheduler: scheduler,
          bus: bus,
          templates: templates,
          daemon: daemon,
          model: model,
          calendar: calendar,
          runtime: runtime,
          terminal: terminal,
          project: project,
          plan: plan,
          search: search,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
