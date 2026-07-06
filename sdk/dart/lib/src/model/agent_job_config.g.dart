// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'agent_job_config.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AgentJobConfig extends AgentJobConfig {
  @override
  final String prompt;
  @override
  final String? context;
  @override
  final String? model;
  @override
  final int? maxTokens;
  @override
  final num? temperature;

  factory _$AgentJobConfig([void Function(AgentJobConfigBuilder)? updates]) =>
      (AgentJobConfigBuilder()..update(updates))._build();

  _$AgentJobConfig._(
      {required this.prompt,
      this.context,
      this.model,
      this.maxTokens,
      this.temperature})
      : super._();
  @override
  AgentJobConfig rebuild(void Function(AgentJobConfigBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AgentJobConfigBuilder toBuilder() => AgentJobConfigBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AgentJobConfig &&
        prompt == other.prompt &&
        context == other.context &&
        model == other.model &&
        maxTokens == other.maxTokens &&
        temperature == other.temperature;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, prompt.hashCode);
    _$hash = $jc(_$hash, context.hashCode);
    _$hash = $jc(_$hash, model.hashCode);
    _$hash = $jc(_$hash, maxTokens.hashCode);
    _$hash = $jc(_$hash, temperature.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AgentJobConfig')
          ..add('prompt', prompt)
          ..add('context', context)
          ..add('model', model)
          ..add('maxTokens', maxTokens)
          ..add('temperature', temperature))
        .toString();
  }
}

class AgentJobConfigBuilder
    implements Builder<AgentJobConfig, AgentJobConfigBuilder> {
  _$AgentJobConfig? _$v;

  String? _prompt;
  String? get prompt => _$this._prompt;
  set prompt(String? prompt) => _$this._prompt = prompt;

  String? _context;
  String? get context => _$this._context;
  set context(String? context) =>
      _$this._context = context;

  String? _model;
  String? get model => _$this._model;
  set model(String? model) =>
      _$this._model = model;

  int? _maxTokens;
  int? get maxTokens => _$this._maxTokens;
  set maxTokens(int? maxTokens) =>
      _$this._maxTokens = maxTokens;

  num? _temperature;
  num? get temperature => _$this._temperature;
  set temperature(num? temperature) =>
      _$this._temperature = temperature;

  AgentJobConfigBuilder() {
    AgentJobConfig._defaults(this);
  }

  AgentJobConfigBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _prompt = $v.prompt;
      _context = $v.context;
      _model = $v.model;
      _maxTokens = $v.maxTokens;
      _temperature = $v.temperature;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AgentJobConfig other) {
    _$v = other as _$AgentJobConfig;
  }

  @override
  void update(void Function(AgentJobConfigBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AgentJobConfig build() => _build();

  _$AgentJobConfig _build() {
    final _$result = _$v ??
        _$AgentJobConfig._(
          prompt: BuiltValueNullFieldError.checkNotNull(
              prompt, r'AgentJobConfig', 'prompt'),
          context: context,
          model: model,
          maxTokens: maxTokens,
          temperature: temperature,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
