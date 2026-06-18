// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'agent_job_config.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AgentJobConfig extends AgentJobConfig {
  @override
  final String prompt;
  @override
  final String? contextCommaOmitempty;
  @override
  final String? modelCommaOmitempty;
  @override
  final int? maxTokensCommaOmitempty;
  @override
  final num? temperatureCommaOmitempty;

  factory _$AgentJobConfig([void Function(AgentJobConfigBuilder)? updates]) =>
      (AgentJobConfigBuilder()..update(updates))._build();

  _$AgentJobConfig._(
      {required this.prompt,
      this.contextCommaOmitempty,
      this.modelCommaOmitempty,
      this.maxTokensCommaOmitempty,
      this.temperatureCommaOmitempty})
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
        contextCommaOmitempty == other.contextCommaOmitempty &&
        modelCommaOmitempty == other.modelCommaOmitempty &&
        maxTokensCommaOmitempty == other.maxTokensCommaOmitempty &&
        temperatureCommaOmitempty == other.temperatureCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, prompt.hashCode);
    _$hash = $jc(_$hash, contextCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, modelCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, maxTokensCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, temperatureCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AgentJobConfig')
          ..add('prompt', prompt)
          ..add('contextCommaOmitempty', contextCommaOmitempty)
          ..add('modelCommaOmitempty', modelCommaOmitempty)
          ..add('maxTokensCommaOmitempty', maxTokensCommaOmitempty)
          ..add('temperatureCommaOmitempty', temperatureCommaOmitempty))
        .toString();
  }
}

class AgentJobConfigBuilder
    implements Builder<AgentJobConfig, AgentJobConfigBuilder> {
  _$AgentJobConfig? _$v;

  String? _prompt;
  String? get prompt => _$this._prompt;
  set prompt(String? prompt) => _$this._prompt = prompt;

  String? _contextCommaOmitempty;
  String? get contextCommaOmitempty => _$this._contextCommaOmitempty;
  set contextCommaOmitempty(String? contextCommaOmitempty) =>
      _$this._contextCommaOmitempty = contextCommaOmitempty;

  String? _modelCommaOmitempty;
  String? get modelCommaOmitempty => _$this._modelCommaOmitempty;
  set modelCommaOmitempty(String? modelCommaOmitempty) =>
      _$this._modelCommaOmitempty = modelCommaOmitempty;

  int? _maxTokensCommaOmitempty;
  int? get maxTokensCommaOmitempty => _$this._maxTokensCommaOmitempty;
  set maxTokensCommaOmitempty(int? maxTokensCommaOmitempty) =>
      _$this._maxTokensCommaOmitempty = maxTokensCommaOmitempty;

  num? _temperatureCommaOmitempty;
  num? get temperatureCommaOmitempty => _$this._temperatureCommaOmitempty;
  set temperatureCommaOmitempty(num? temperatureCommaOmitempty) =>
      _$this._temperatureCommaOmitempty = temperatureCommaOmitempty;

  AgentJobConfigBuilder() {
    AgentJobConfig._defaults(this);
  }

  AgentJobConfigBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _prompt = $v.prompt;
      _contextCommaOmitempty = $v.contextCommaOmitempty;
      _modelCommaOmitempty = $v.modelCommaOmitempty;
      _maxTokensCommaOmitempty = $v.maxTokensCommaOmitempty;
      _temperatureCommaOmitempty = $v.temperatureCommaOmitempty;
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
          contextCommaOmitempty: contextCommaOmitempty,
          modelCommaOmitempty: modelCommaOmitempty,
          maxTokensCommaOmitempty: maxTokensCommaOmitempty,
          temperatureCommaOmitempty: temperatureCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
