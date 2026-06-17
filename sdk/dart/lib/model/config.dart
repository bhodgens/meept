//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

// Standalone model file
import 'dart:convert' show json;
import 'package:collection/collection.dart';

class Config {
  /// Returns a new [Config] instance.
  Config({
    this.bus,
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
    this.planStore,
  });

  Object? bus;

  Object? agentRegistry;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? queue;

  Object? memoryManager;

  Object? taskRegistry;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? sessionStore;

  Object? workerPool;

  Object? skillRegistry;

  Object? skillExecutor;

  Object? templateRegistry;

  Object? selfImprove;

  Object? tokenCache;

  Object? securityChecker;

  Object? scheduler;

  Object? calendarClient;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? daemonController;

  Object? runtimeManager;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? workingDir;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? pidFile;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? stateDir;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? binPath;

  Object? projectManager;

  Object? planManager;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? planStore;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Config &&
    other.bus == bus &&
    other.agentRegistry == agentRegistry &&
    other.queue == queue &&
    other.memoryManager == memoryManager &&
    other.taskRegistry == taskRegistry &&
    other.sessionStore == sessionStore &&
    other.workerPool == workerPool &&
    other.skillRegistry == skillRegistry &&
    other.skillExecutor == skillExecutor &&
    other.templateRegistry == templateRegistry &&
    other.selfImprove == selfImprove &&
    other.tokenCache == tokenCache &&
    other.securityChecker == securityChecker &&
    other.scheduler == scheduler &&
    other.calendarClient == calendarClient &&
    other.daemonController == daemonController &&
    other.runtimeManager == runtimeManager &&
    other.workingDir == workingDir &&
    other.pidFile == pidFile &&
    other.stateDir == stateDir &&
    other.binPath == binPath &&
    other.projectManager == projectManager &&
    other.planManager == planManager &&
    other.planStore == planStore;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (bus == null ? 0 : bus!.hashCode) +
    (agentRegistry == null ? 0 : agentRegistry!.hashCode) +
    (queue == null ? 0 : queue!.hashCode) +
    (memoryManager == null ? 0 : memoryManager!.hashCode) +
    (taskRegistry == null ? 0 : taskRegistry!.hashCode) +
    (sessionStore == null ? 0 : sessionStore!.hashCode) +
    (workerPool == null ? 0 : workerPool!.hashCode) +
    (skillRegistry == null ? 0 : skillRegistry!.hashCode) +
    (skillExecutor == null ? 0 : skillExecutor!.hashCode) +
    (templateRegistry == null ? 0 : templateRegistry!.hashCode) +
    (selfImprove == null ? 0 : selfImprove!.hashCode) +
    (tokenCache == null ? 0 : tokenCache!.hashCode) +
    (securityChecker == null ? 0 : securityChecker!.hashCode) +
    (scheduler == null ? 0 : scheduler!.hashCode) +
    (calendarClient == null ? 0 : calendarClient!.hashCode) +
    (daemonController == null ? 0 : daemonController!.hashCode) +
    (runtimeManager == null ? 0 : runtimeManager!.hashCode) +
    (workingDir == null ? 0 : workingDir!.hashCode) +
    (pidFile == null ? 0 : pidFile!.hashCode) +
    (stateDir == null ? 0 : stateDir!.hashCode) +
    (binPath == null ? 0 : binPath!.hashCode) +
    (projectManager == null ? 0 : projectManager!.hashCode) +
    (planManager == null ? 0 : planManager!.hashCode) +
    (planStore == null ? 0 : planStore!.hashCode);

  @override
  String toString() => 'Config[bus=$bus, agentRegistry=$agentRegistry, queue=$queue, memoryManager=$memoryManager, taskRegistry=$taskRegistry, sessionStore=$sessionStore, workerPool=$workerPool, skillRegistry=$skillRegistry, skillExecutor=$skillExecutor, templateRegistry=$templateRegistry, selfImprove=$selfImprove, tokenCache=$tokenCache, securityChecker=$securityChecker, scheduler=$scheduler, calendarClient=$calendarClient, daemonController=$daemonController, runtimeManager=$runtimeManager, workingDir=$workingDir, pidFile=$pidFile, stateDir=$stateDir, binPath=$binPath, projectManager=$projectManager, planManager=$planManager, planStore=$planStore]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.bus != null) {
      json[r'Bus'] = this.bus;
    } else {
      json[r'Bus'] = null;
    }
    if (this.agentRegistry != null) {
      json[r'AgentRegistry'] = this.agentRegistry;
    } else {
      json[r'AgentRegistry'] = null;
    }
    if (this.queue != null) {
      json[r'Queue'] = this.queue;
    } else {
      json[r'Queue'] = null;
    }
    if (this.memoryManager != null) {
      json[r'MemoryManager'] = this.memoryManager;
    } else {
      json[r'MemoryManager'] = null;
    }
    if (this.taskRegistry != null) {
      json[r'TaskRegistry'] = this.taskRegistry;
    } else {
      json[r'TaskRegistry'] = null;
    }
    if (this.sessionStore != null) {
      json[r'SessionStore'] = this.sessionStore;
    } else {
      json[r'SessionStore'] = null;
    }
    if (this.workerPool != null) {
      json[r'WorkerPool'] = this.workerPool;
    } else {
      json[r'WorkerPool'] = null;
    }
    if (this.skillRegistry != null) {
      json[r'SkillRegistry'] = this.skillRegistry;
    } else {
      json[r'SkillRegistry'] = null;
    }
    if (this.skillExecutor != null) {
      json[r'SkillExecutor'] = this.skillExecutor;
    } else {
      json[r'SkillExecutor'] = null;
    }
    if (this.templateRegistry != null) {
      json[r'TemplateRegistry'] = this.templateRegistry;
    } else {
      json[r'TemplateRegistry'] = null;
    }
    if (this.selfImprove != null) {
      json[r'SelfImprove'] = this.selfImprove;
    } else {
      json[r'SelfImprove'] = null;
    }
    if (this.tokenCache != null) {
      json[r'TokenCache'] = this.tokenCache;
    } else {
      json[r'TokenCache'] = null;
    }
    if (this.securityChecker != null) {
      json[r'SecurityChecker'] = this.securityChecker;
    } else {
      json[r'SecurityChecker'] = null;
    }
    if (this.scheduler != null) {
      json[r'Scheduler'] = this.scheduler;
    } else {
      json[r'Scheduler'] = null;
    }
    if (this.calendarClient != null) {
      json[r'CalendarClient'] = this.calendarClient;
    } else {
      json[r'CalendarClient'] = null;
    }
    if (this.daemonController != null) {
      json[r'DaemonController'] = this.daemonController;
    } else {
      json[r'DaemonController'] = null;
    }
    if (this.runtimeManager != null) {
      json[r'RuntimeManager'] = this.runtimeManager;
    } else {
      json[r'RuntimeManager'] = null;
    }
    if (this.workingDir != null) {
      json[r'WorkingDir'] = this.workingDir;
    } else {
      json[r'WorkingDir'] = null;
    }
    if (this.pidFile != null) {
      json[r'PidFile'] = this.pidFile;
    } else {
      json[r'PidFile'] = null;
    }
    if (this.stateDir != null) {
      json[r'StateDir'] = this.stateDir;
    } else {
      json[r'StateDir'] = null;
    }
    if (this.binPath != null) {
      json[r'BinPath'] = this.binPath;
    } else {
      json[r'BinPath'] = null;
    }
    if (this.projectManager != null) {
      json[r'ProjectManager'] = this.projectManager;
    } else {
      json[r'ProjectManager'] = null;
    }
    if (this.planManager != null) {
      json[r'PlanManager'] = this.planManager;
    } else {
      json[r'PlanManager'] = null;
    }
    if (this.planStore != null) {
      json[r'PlanStore'] = this.planStore;
    } else {
      json[r'PlanStore'] = null;
    }
    return json;
  }

  /// Returns a new [Config] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Config? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return Config(
        bus: mapValueOfType<Object>(json, r'Bus'),
        agentRegistry: mapValueOfType<Object>(json, r'AgentRegistry'),
        queue: mapValueOfType<Object>(json, r'Queue'),
        memoryManager: mapValueOfType<Object>(json, r'MemoryManager'),
        taskRegistry: mapValueOfType<Object>(json, r'TaskRegistry'),
        sessionStore: mapValueOfType<Object>(json, r'SessionStore'),
        workerPool: mapValueOfType<Object>(json, r'WorkerPool'),
        skillRegistry: mapValueOfType<Object>(json, r'SkillRegistry'),
        skillExecutor: mapValueOfType<Object>(json, r'SkillExecutor'),
        templateRegistry: mapValueOfType<Object>(json, r'TemplateRegistry'),
        selfImprove: mapValueOfType<Object>(json, r'SelfImprove'),
        tokenCache: mapValueOfType<Object>(json, r'TokenCache'),
        securityChecker: mapValueOfType<Object>(json, r'SecurityChecker'),
        scheduler: mapValueOfType<Object>(json, r'Scheduler'),
        calendarClient: mapValueOfType<Object>(json, r'CalendarClient'),
        daemonController: mapValueOfType<Object>(json, r'DaemonController'),
        runtimeManager: mapValueOfType<Object>(json, r'RuntimeManager'),
        workingDir: mapValueOfType<String>(json, r'WorkingDir'),
        pidFile: mapValueOfType<String>(json, r'PidFile'),
        stateDir: mapValueOfType<String>(json, r'StateDir'),
        binPath: mapValueOfType<String>(json, r'BinPath'),
        projectManager: mapValueOfType<Object>(json, r'ProjectManager'),
        planManager: mapValueOfType<Object>(json, r'PlanManager'),
        planStore: mapValueOfType<Object>(json, r'PlanStore'),
      );
    }
    return null;
  }

  static List<Config> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Config>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Config.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Config> mapFromJson(dynamic json) {
    final map = <String, Config>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Config.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Config-objects as value to a dart map
  static Map<String, List<Config>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Config>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Config.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

