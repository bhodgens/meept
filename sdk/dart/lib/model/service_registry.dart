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

class ServiceRegistry {
  /// Returns a new [ServiceRegistry] instance.
  ServiceRegistry({
    this.chat,
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
    this.search,
  });

  Object? chat;

  Object? memory;

  Object? task;

  Object? queue;

  Object? session;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Object? sessionStore;

  Object? worker;

  Object? pipeline;

  Object? skills;

  Object? selfImprove;

  Object? cache;

  Object? security;

  Object? scheduler;

  Object? bus;

  Object? templates;

  Object? daemon;

  Object? model;

  Object? calendar;

  Object? runtime;

  Object? terminal;

  Object? project;

  Object? plan;

  Object? search;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ServiceRegistry &&
    other.chat == chat &&
    other.memory == memory &&
    other.task == task &&
    other.queue == queue &&
    other.session == session &&
    other.sessionStore == sessionStore &&
    other.worker == worker &&
    other.pipeline == pipeline &&
    other.skills == skills &&
    other.selfImprove == selfImprove &&
    other.cache == cache &&
    other.security == security &&
    other.scheduler == scheduler &&
    other.bus == bus &&
    other.templates == templates &&
    other.daemon == daemon &&
    other.model == model &&
    other.calendar == calendar &&
    other.runtime == runtime &&
    other.terminal == terminal &&
    other.project == project &&
    other.plan == plan &&
    other.search == search;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (chat == null ? 0 : chat!.hashCode) +
    (memory == null ? 0 : memory!.hashCode) +
    (task == null ? 0 : task!.hashCode) +
    (queue == null ? 0 : queue!.hashCode) +
    (session == null ? 0 : session!.hashCode) +
    (sessionStore == null ? 0 : sessionStore!.hashCode) +
    (worker == null ? 0 : worker!.hashCode) +
    (pipeline == null ? 0 : pipeline!.hashCode) +
    (skills == null ? 0 : skills!.hashCode) +
    (selfImprove == null ? 0 : selfImprove!.hashCode) +
    (cache == null ? 0 : cache!.hashCode) +
    (security == null ? 0 : security!.hashCode) +
    (scheduler == null ? 0 : scheduler!.hashCode) +
    (bus == null ? 0 : bus!.hashCode) +
    (templates == null ? 0 : templates!.hashCode) +
    (daemon == null ? 0 : daemon!.hashCode) +
    (model == null ? 0 : model!.hashCode) +
    (calendar == null ? 0 : calendar!.hashCode) +
    (runtime == null ? 0 : runtime!.hashCode) +
    (terminal == null ? 0 : terminal!.hashCode) +
    (project == null ? 0 : project!.hashCode) +
    (plan == null ? 0 : plan!.hashCode) +
    (search == null ? 0 : search!.hashCode);

  @override
  String toString() => 'ServiceRegistry[chat=$chat, memory=$memory, task=$task, queue=$queue, session=$session, sessionStore=$sessionStore, worker=$worker, pipeline=$pipeline, skills=$skills, selfImprove=$selfImprove, cache=$cache, security=$security, scheduler=$scheduler, bus=$bus, templates=$templates, daemon=$daemon, model=$model, calendar=$calendar, runtime=$runtime, terminal=$terminal, project=$project, plan=$plan, search=$search]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.chat != null) {
      json[r'Chat'] = this.chat;
    } else {
      json[r'Chat'] = null;
    }
    if (this.memory != null) {
      json[r'Memory'] = this.memory;
    } else {
      json[r'Memory'] = null;
    }
    if (this.task != null) {
      json[r'Task'] = this.task;
    } else {
      json[r'Task'] = null;
    }
    if (this.queue != null) {
      json[r'Queue'] = this.queue;
    } else {
      json[r'Queue'] = null;
    }
    if (this.session != null) {
      json[r'Session'] = this.session;
    } else {
      json[r'Session'] = null;
    }
    if (this.sessionStore != null) {
      json[r'SessionStore'] = this.sessionStore;
    } else {
      json[r'SessionStore'] = null;
    }
    if (this.worker != null) {
      json[r'Worker'] = this.worker;
    } else {
      json[r'Worker'] = null;
    }
    if (this.pipeline != null) {
      json[r'Pipeline'] = this.pipeline;
    } else {
      json[r'Pipeline'] = null;
    }
    if (this.skills != null) {
      json[r'Skills'] = this.skills;
    } else {
      json[r'Skills'] = null;
    }
    if (this.selfImprove != null) {
      json[r'SelfImprove'] = this.selfImprove;
    } else {
      json[r'SelfImprove'] = null;
    }
    if (this.cache != null) {
      json[r'Cache'] = this.cache;
    } else {
      json[r'Cache'] = null;
    }
    if (this.security != null) {
      json[r'Security'] = this.security;
    } else {
      json[r'Security'] = null;
    }
    if (this.scheduler != null) {
      json[r'Scheduler'] = this.scheduler;
    } else {
      json[r'Scheduler'] = null;
    }
    if (this.bus != null) {
      json[r'Bus'] = this.bus;
    } else {
      json[r'Bus'] = null;
    }
    if (this.templates != null) {
      json[r'Templates'] = this.templates;
    } else {
      json[r'Templates'] = null;
    }
    if (this.daemon != null) {
      json[r'Daemon'] = this.daemon;
    } else {
      json[r'Daemon'] = null;
    }
    if (this.model != null) {
      json[r'Model'] = this.model;
    } else {
      json[r'Model'] = null;
    }
    if (this.calendar != null) {
      json[r'Calendar'] = this.calendar;
    } else {
      json[r'Calendar'] = null;
    }
    if (this.runtime != null) {
      json[r'Runtime'] = this.runtime;
    } else {
      json[r'Runtime'] = null;
    }
    if (this.terminal != null) {
      json[r'Terminal'] = this.terminal;
    } else {
      json[r'Terminal'] = null;
    }
    if (this.project != null) {
      json[r'Project'] = this.project;
    } else {
      json[r'Project'] = null;
    }
    if (this.plan != null) {
      json[r'Plan'] = this.plan;
    } else {
      json[r'Plan'] = null;
    }
    if (this.search != null) {
      json[r'Search'] = this.search;
    } else {
      json[r'Search'] = null;
    }
    return json;
  }

  /// Returns a new [ServiceRegistry] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ServiceRegistry? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ServiceRegistry(
        chat: mapValueOfType<Object>(json, r'Chat'),
        memory: mapValueOfType<Object>(json, r'Memory'),
        task: mapValueOfType<Object>(json, r'Task'),
        queue: mapValueOfType<Object>(json, r'Queue'),
        session: mapValueOfType<Object>(json, r'Session'),
        sessionStore: mapValueOfType<Object>(json, r'SessionStore'),
        worker: mapValueOfType<Object>(json, r'Worker'),
        pipeline: mapValueOfType<Object>(json, r'Pipeline'),
        skills: mapValueOfType<Object>(json, r'Skills'),
        selfImprove: mapValueOfType<Object>(json, r'SelfImprove'),
        cache: mapValueOfType<Object>(json, r'Cache'),
        security: mapValueOfType<Object>(json, r'Security'),
        scheduler: mapValueOfType<Object>(json, r'Scheduler'),
        bus: mapValueOfType<Object>(json, r'Bus'),
        templates: mapValueOfType<Object>(json, r'Templates'),
        daemon: mapValueOfType<Object>(json, r'Daemon'),
        model: mapValueOfType<Object>(json, r'Model'),
        calendar: mapValueOfType<Object>(json, r'Calendar'),
        runtime: mapValueOfType<Object>(json, r'Runtime'),
        terminal: mapValueOfType<Object>(json, r'Terminal'),
        project: mapValueOfType<Object>(json, r'Project'),
        plan: mapValueOfType<Object>(json, r'Plan'),
        search: mapValueOfType<Object>(json, r'Search'),
      );
    }
    return null;
  }

  static List<ServiceRegistry> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ServiceRegistry>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ServiceRegistry.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ServiceRegistry> mapFromJson(dynamic json) {
    final map = <String, ServiceRegistry>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ServiceRegistry.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ServiceRegistry-objects as value to a dart map
  static Map<String, List<ServiceRegistry>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ServiceRegistry>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ServiceRegistry.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

