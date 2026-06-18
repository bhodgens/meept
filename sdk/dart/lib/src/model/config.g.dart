// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'config.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$Config extends Config {
  @override
  final JsonObject? bus;
  @override
  final JsonObject? agentRegistry;
  @override
  final JsonObject? queue;
  @override
  final JsonObject? memoryManager;
  @override
  final JsonObject? taskRegistry;
  @override
  final JsonObject? sessionStore;
  @override
  final JsonObject? workerPool;
  @override
  final JsonObject? skillRegistry;
  @override
  final JsonObject? skillExecutor;
  @override
  final JsonObject? templateRegistry;
  @override
  final JsonObject? selfImprove;
  @override
  final JsonObject? tokenCache;
  @override
  final JsonObject? securityChecker;
  @override
  final JsonObject? scheduler;
  @override
  final JsonObject? calendarClient;
  @override
  final JsonObject? daemonController;
  @override
  final JsonObject? runtimeManager;
  @override
  final String? workingDir;
  @override
  final String? pidFile;
  @override
  final String? stateDir;
  @override
  final String? binPath;
  @override
  final JsonObject? projectManager;
  @override
  final JsonObject? planManager;
  @override
  final JsonObject? planStore;

  factory _$Config([void Function(ConfigBuilder)? updates]) =>
      (ConfigBuilder()..update(updates))._build();

  _$Config._(
      {this.bus,
      this.agentRegistry,
      this.queue,
      this.memoryManager,
      this.taskRegistry,
      this.sessionStore,
      this.workerPool,
      this.skillRegistry,
      this.skillExecutor,
      this.templateRegistry,
      this.selfImprove,
      this.tokenCache,
      this.securityChecker,
      this.scheduler,
      this.calendarClient,
      this.daemonController,
      this.runtimeManager,
      this.workingDir,
      this.pidFile,
      this.stateDir,
      this.binPath,
      this.projectManager,
      this.planManager,
      this.planStore})
      : super._();
  @override
  Config rebuild(void Function(ConfigBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ConfigBuilder toBuilder() => ConfigBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is Config &&
        bus == other.bus &&
        agentRegistry == other.agentRegistry &&
        queue == other.queue &&
        memoryManager == other.memoryManager &&
        taskRegistry == other.taskRegistry &&
        sessionStore == other.sessionStore &&
        workerPool == other.workerPool &&
        skillRegistry == other.skillRegistry &&
        skillExecutor == other.skillExecutor &&
        templateRegistry == other.templateRegistry &&
        selfImprove == other.selfImprove &&
        tokenCache == other.tokenCache &&
        securityChecker == other.securityChecker &&
        scheduler == other.scheduler &&
        calendarClient == other.calendarClient &&
        daemonController == other.daemonController &&
        runtimeManager == other.runtimeManager &&
        workingDir == other.workingDir &&
        pidFile == other.pidFile &&
        stateDir == other.stateDir &&
        binPath == other.binPath &&
        projectManager == other.projectManager &&
        planManager == other.planManager &&
        planStore == other.planStore;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, bus.hashCode);
    _$hash = $jc(_$hash, agentRegistry.hashCode);
    _$hash = $jc(_$hash, queue.hashCode);
    _$hash = $jc(_$hash, memoryManager.hashCode);
    _$hash = $jc(_$hash, taskRegistry.hashCode);
    _$hash = $jc(_$hash, sessionStore.hashCode);
    _$hash = $jc(_$hash, workerPool.hashCode);
    _$hash = $jc(_$hash, skillRegistry.hashCode);
    _$hash = $jc(_$hash, skillExecutor.hashCode);
    _$hash = $jc(_$hash, templateRegistry.hashCode);
    _$hash = $jc(_$hash, selfImprove.hashCode);
    _$hash = $jc(_$hash, tokenCache.hashCode);
    _$hash = $jc(_$hash, securityChecker.hashCode);
    _$hash = $jc(_$hash, scheduler.hashCode);
    _$hash = $jc(_$hash, calendarClient.hashCode);
    _$hash = $jc(_$hash, daemonController.hashCode);
    _$hash = $jc(_$hash, runtimeManager.hashCode);
    _$hash = $jc(_$hash, workingDir.hashCode);
    _$hash = $jc(_$hash, pidFile.hashCode);
    _$hash = $jc(_$hash, stateDir.hashCode);
    _$hash = $jc(_$hash, binPath.hashCode);
    _$hash = $jc(_$hash, projectManager.hashCode);
    _$hash = $jc(_$hash, planManager.hashCode);
    _$hash = $jc(_$hash, planStore.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'Config')
          ..add('bus', bus)
          ..add('agentRegistry', agentRegistry)
          ..add('queue', queue)
          ..add('memoryManager', memoryManager)
          ..add('taskRegistry', taskRegistry)
          ..add('sessionStore', sessionStore)
          ..add('workerPool', workerPool)
          ..add('skillRegistry', skillRegistry)
          ..add('skillExecutor', skillExecutor)
          ..add('templateRegistry', templateRegistry)
          ..add('selfImprove', selfImprove)
          ..add('tokenCache', tokenCache)
          ..add('securityChecker', securityChecker)
          ..add('scheduler', scheduler)
          ..add('calendarClient', calendarClient)
          ..add('daemonController', daemonController)
          ..add('runtimeManager', runtimeManager)
          ..add('workingDir', workingDir)
          ..add('pidFile', pidFile)
          ..add('stateDir', stateDir)
          ..add('binPath', binPath)
          ..add('projectManager', projectManager)
          ..add('planManager', planManager)
          ..add('planStore', planStore))
        .toString();
  }
}

class ConfigBuilder implements Builder<Config, ConfigBuilder> {
  _$Config? _$v;

  JsonObject? _bus;
  JsonObject? get bus => _$this._bus;
  set bus(JsonObject? bus) => _$this._bus = bus;

  JsonObject? _agentRegistry;
  JsonObject? get agentRegistry => _$this._agentRegistry;
  set agentRegistry(JsonObject? agentRegistry) =>
      _$this._agentRegistry = agentRegistry;

  JsonObject? _queue;
  JsonObject? get queue => _$this._queue;
  set queue(JsonObject? queue) => _$this._queue = queue;

  JsonObject? _memoryManager;
  JsonObject? get memoryManager => _$this._memoryManager;
  set memoryManager(JsonObject? memoryManager) =>
      _$this._memoryManager = memoryManager;

  JsonObject? _taskRegistry;
  JsonObject? get taskRegistry => _$this._taskRegistry;
  set taskRegistry(JsonObject? taskRegistry) =>
      _$this._taskRegistry = taskRegistry;

  JsonObject? _sessionStore;
  JsonObject? get sessionStore => _$this._sessionStore;
  set sessionStore(JsonObject? sessionStore) =>
      _$this._sessionStore = sessionStore;

  JsonObject? _workerPool;
  JsonObject? get workerPool => _$this._workerPool;
  set workerPool(JsonObject? workerPool) => _$this._workerPool = workerPool;

  JsonObject? _skillRegistry;
  JsonObject? get skillRegistry => _$this._skillRegistry;
  set skillRegistry(JsonObject? skillRegistry) =>
      _$this._skillRegistry = skillRegistry;

  JsonObject? _skillExecutor;
  JsonObject? get skillExecutor => _$this._skillExecutor;
  set skillExecutor(JsonObject? skillExecutor) =>
      _$this._skillExecutor = skillExecutor;

  JsonObject? _templateRegistry;
  JsonObject? get templateRegistry => _$this._templateRegistry;
  set templateRegistry(JsonObject? templateRegistry) =>
      _$this._templateRegistry = templateRegistry;

  JsonObject? _selfImprove;
  JsonObject? get selfImprove => _$this._selfImprove;
  set selfImprove(JsonObject? selfImprove) => _$this._selfImprove = selfImprove;

  JsonObject? _tokenCache;
  JsonObject? get tokenCache => _$this._tokenCache;
  set tokenCache(JsonObject? tokenCache) => _$this._tokenCache = tokenCache;

  JsonObject? _securityChecker;
  JsonObject? get securityChecker => _$this._securityChecker;
  set securityChecker(JsonObject? securityChecker) =>
      _$this._securityChecker = securityChecker;

  JsonObject? _scheduler;
  JsonObject? get scheduler => _$this._scheduler;
  set scheduler(JsonObject? scheduler) => _$this._scheduler = scheduler;

  JsonObject? _calendarClient;
  JsonObject? get calendarClient => _$this._calendarClient;
  set calendarClient(JsonObject? calendarClient) =>
      _$this._calendarClient = calendarClient;

  JsonObject? _daemonController;
  JsonObject? get daemonController => _$this._daemonController;
  set daemonController(JsonObject? daemonController) =>
      _$this._daemonController = daemonController;

  JsonObject? _runtimeManager;
  JsonObject? get runtimeManager => _$this._runtimeManager;
  set runtimeManager(JsonObject? runtimeManager) =>
      _$this._runtimeManager = runtimeManager;

  String? _workingDir;
  String? get workingDir => _$this._workingDir;
  set workingDir(String? workingDir) => _$this._workingDir = workingDir;

  String? _pidFile;
  String? get pidFile => _$this._pidFile;
  set pidFile(String? pidFile) => _$this._pidFile = pidFile;

  String? _stateDir;
  String? get stateDir => _$this._stateDir;
  set stateDir(String? stateDir) => _$this._stateDir = stateDir;

  String? _binPath;
  String? get binPath => _$this._binPath;
  set binPath(String? binPath) => _$this._binPath = binPath;

  JsonObject? _projectManager;
  JsonObject? get projectManager => _$this._projectManager;
  set projectManager(JsonObject? projectManager) =>
      _$this._projectManager = projectManager;

  JsonObject? _planManager;
  JsonObject? get planManager => _$this._planManager;
  set planManager(JsonObject? planManager) => _$this._planManager = planManager;

  JsonObject? _planStore;
  JsonObject? get planStore => _$this._planStore;
  set planStore(JsonObject? planStore) => _$this._planStore = planStore;

  ConfigBuilder() {
    Config._defaults(this);
  }

  ConfigBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _bus = $v.bus;
      _agentRegistry = $v.agentRegistry;
      _queue = $v.queue;
      _memoryManager = $v.memoryManager;
      _taskRegistry = $v.taskRegistry;
      _sessionStore = $v.sessionStore;
      _workerPool = $v.workerPool;
      _skillRegistry = $v.skillRegistry;
      _skillExecutor = $v.skillExecutor;
      _templateRegistry = $v.templateRegistry;
      _selfImprove = $v.selfImprove;
      _tokenCache = $v.tokenCache;
      _securityChecker = $v.securityChecker;
      _scheduler = $v.scheduler;
      _calendarClient = $v.calendarClient;
      _daemonController = $v.daemonController;
      _runtimeManager = $v.runtimeManager;
      _workingDir = $v.workingDir;
      _pidFile = $v.pidFile;
      _stateDir = $v.stateDir;
      _binPath = $v.binPath;
      _projectManager = $v.projectManager;
      _planManager = $v.planManager;
      _planStore = $v.planStore;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(Config other) {
    _$v = other as _$Config;
  }

  @override
  void update(void Function(ConfigBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  Config build() => _build();

  _$Config _build() {
    final _$result = _$v ??
        _$Config._(
          bus: bus,
          agentRegistry: agentRegistry,
          queue: queue,
          memoryManager: memoryManager,
          taskRegistry: taskRegistry,
          sessionStore: sessionStore,
          workerPool: workerPool,
          skillRegistry: skillRegistry,
          skillExecutor: skillExecutor,
          templateRegistry: templateRegistry,
          selfImprove: selfImprove,
          tokenCache: tokenCache,
          securityChecker: securityChecker,
          scheduler: scheduler,
          calendarClient: calendarClient,
          daemonController: daemonController,
          runtimeManager: runtimeManager,
          workingDir: workingDir,
          pidFile: pidFile,
          stateDir: stateDir,
          binPath: binPath,
          projectManager: projectManager,
          planManager: planManager,
          planStore: planStore,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
