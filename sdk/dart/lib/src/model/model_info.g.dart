// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'model_info.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ModelInfo extends ModelInfo {
  @override
  final String provider;
  @override
  final String model;
  @override
  final String fullName;
  @override
  final String baseUrl;
  @override
  final int contextLimit;
  @override
  final int maxOutput;
  @override
  final String? capabilities;
  @override
  final bool isDefault;
  @override
  final num inputCost;
  @override
  final num outputCost;

  factory _$ModelInfo([void Function(ModelInfoBuilder)? updates]) =>
      (ModelInfoBuilder()..update(updates))._build();

  _$ModelInfo._(
      {required this.provider,
      required this.model,
      required this.fullName,
      required this.baseUrl,
      required this.contextLimit,
      required this.maxOutput,
      this.capabilities,
      required this.isDefault,
      required this.inputCost,
      required this.outputCost})
      : super._();
  @override
  ModelInfo rebuild(void Function(ModelInfoBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ModelInfoBuilder toBuilder() => ModelInfoBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ModelInfo &&
        provider == other.provider &&
        model == other.model &&
        fullName == other.fullName &&
        baseUrl == other.baseUrl &&
        contextLimit == other.contextLimit &&
        maxOutput == other.maxOutput &&
        capabilities == other.capabilities &&
        isDefault == other.isDefault &&
        inputCost == other.inputCost &&
        outputCost == other.outputCost;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, provider.hashCode);
    _$hash = $jc(_$hash, model.hashCode);
    _$hash = $jc(_$hash, fullName.hashCode);
    _$hash = $jc(_$hash, baseUrl.hashCode);
    _$hash = $jc(_$hash, contextLimit.hashCode);
    _$hash = $jc(_$hash, maxOutput.hashCode);
    _$hash = $jc(_$hash, capabilities.hashCode);
    _$hash = $jc(_$hash, isDefault.hashCode);
    _$hash = $jc(_$hash, inputCost.hashCode);
    _$hash = $jc(_$hash, outputCost.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ModelInfo')
          ..add('provider', provider)
          ..add('model', model)
          ..add('fullName', fullName)
          ..add('baseUrl', baseUrl)
          ..add('contextLimit', contextLimit)
          ..add('maxOutput', maxOutput)
          ..add('capabilities', capabilities)
          ..add('isDefault', isDefault)
          ..add('inputCost', inputCost)
          ..add('outputCost', outputCost))
        .toString();
  }
}

class ModelInfoBuilder implements Builder<ModelInfo, ModelInfoBuilder> {
  _$ModelInfo? _$v;

  String? _provider;
  String? get provider => _$this._provider;
  set provider(String? provider) => _$this._provider = provider;

  String? _model;
  String? get model => _$this._model;
  set model(String? model) => _$this._model = model;

  String? _fullName;
  String? get fullName => _$this._fullName;
  set fullName(String? fullName) => _$this._fullName = fullName;

  String? _baseUrl;
  String? get baseUrl => _$this._baseUrl;
  set baseUrl(String? baseUrl) => _$this._baseUrl = baseUrl;

  int? _contextLimit;
  int? get contextLimit => _$this._contextLimit;
  set contextLimit(int? contextLimit) => _$this._contextLimit = contextLimit;

  int? _maxOutput;
  int? get maxOutput => _$this._maxOutput;
  set maxOutput(int? maxOutput) => _$this._maxOutput = maxOutput;

  String? _capabilities;
  String? get capabilities => _$this._capabilities;
  set capabilities(String? capabilities) => _$this._capabilities = capabilities;

  bool? _isDefault;
  bool? get isDefault => _$this._isDefault;
  set isDefault(bool? isDefault) => _$this._isDefault = isDefault;

  num? _inputCost;
  num? get inputCost => _$this._inputCost;
  set inputCost(num? inputCost) => _$this._inputCost = inputCost;

  num? _outputCost;
  num? get outputCost => _$this._outputCost;
  set outputCost(num? outputCost) => _$this._outputCost = outputCost;

  ModelInfoBuilder() {
    ModelInfo._defaults(this);
  }

  ModelInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _provider = $v.provider;
      _model = $v.model;
      _fullName = $v.fullName;
      _baseUrl = $v.baseUrl;
      _contextLimit = $v.contextLimit;
      _maxOutput = $v.maxOutput;
      _capabilities = $v.capabilities;
      _isDefault = $v.isDefault;
      _inputCost = $v.inputCost;
      _outputCost = $v.outputCost;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ModelInfo other) {
    _$v = other as _$ModelInfo;
  }

  @override
  void update(void Function(ModelInfoBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ModelInfo build() => _build();

  _$ModelInfo _build() {
    final _$result = _$v ??
        _$ModelInfo._(
          provider: BuiltValueNullFieldError.checkNotNull(
              provider, r'ModelInfo', 'provider'),
          model: BuiltValueNullFieldError.checkNotNull(
              model, r'ModelInfo', 'model'),
          fullName: BuiltValueNullFieldError.checkNotNull(
              fullName, r'ModelInfo', 'fullName'),
          baseUrl: BuiltValueNullFieldError.checkNotNull(
              baseUrl, r'ModelInfo', 'baseUrl'),
          contextLimit: BuiltValueNullFieldError.checkNotNull(
              contextLimit, r'ModelInfo', 'contextLimit'),
          maxOutput: BuiltValueNullFieldError.checkNotNull(
              maxOutput, r'ModelInfo', 'maxOutput'),
          capabilities: capabilities,
          isDefault: BuiltValueNullFieldError.checkNotNull(
              isDefault, r'ModelInfo', 'isDefault'),
          inputCost: BuiltValueNullFieldError.checkNotNull(
              inputCost, r'ModelInfo', 'inputCost'),
          outputCost: BuiltValueNullFieldError.checkNotNull(
              outputCost, r'ModelInfo', 'outputCost'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
