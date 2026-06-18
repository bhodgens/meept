// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pipeline_service.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PipelineService extends PipelineService {
  @override
  final JsonObject? mu;
  @override
  final String? pipelines;

  factory _$PipelineService([void Function(PipelineServiceBuilder)? updates]) =>
      (PipelineServiceBuilder()..update(updates))._build();

  _$PipelineService._({this.mu, this.pipelines}) : super._();
  @override
  PipelineService rebuild(void Function(PipelineServiceBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PipelineServiceBuilder toBuilder() => PipelineServiceBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PipelineService &&
        mu == other.mu &&
        pipelines == other.pipelines;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, mu.hashCode);
    _$hash = $jc(_$hash, pipelines.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PipelineService')
          ..add('mu', mu)
          ..add('pipelines', pipelines))
        .toString();
  }
}

class PipelineServiceBuilder
    implements Builder<PipelineService, PipelineServiceBuilder> {
  _$PipelineService? _$v;

  JsonObject? _mu;
  JsonObject? get mu => _$this._mu;
  set mu(JsonObject? mu) => _$this._mu = mu;

  String? _pipelines;
  String? get pipelines => _$this._pipelines;
  set pipelines(String? pipelines) => _$this._pipelines = pipelines;

  PipelineServiceBuilder() {
    PipelineService._defaults(this);
  }

  PipelineServiceBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _mu = $v.mu;
      _pipelines = $v.pipelines;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PipelineService other) {
    _$v = other as _$PipelineService;
  }

  @override
  void update(void Function(PipelineServiceBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PipelineService build() => _build();

  _$PipelineService _build() {
    final _$result = _$v ??
        _$PipelineService._(
          mu: mu,
          pipelines: pipelines,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
