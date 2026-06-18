// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'add_worker_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AddWorkerRequest extends AddWorkerRequest {
  @override
  final String id;
  @override
  final String? capabilities;

  factory _$AddWorkerRequest(
          [void Function(AddWorkerRequestBuilder)? updates]) =>
      (AddWorkerRequestBuilder()..update(updates))._build();

  _$AddWorkerRequest._({required this.id, this.capabilities}) : super._();
  @override
  AddWorkerRequest rebuild(void Function(AddWorkerRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AddWorkerRequestBuilder toBuilder() =>
      AddWorkerRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AddWorkerRequest &&
        id == other.id &&
        capabilities == other.capabilities;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, capabilities.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AddWorkerRequest')
          ..add('id', id)
          ..add('capabilities', capabilities))
        .toString();
  }
}

class AddWorkerRequestBuilder
    implements Builder<AddWorkerRequest, AddWorkerRequestBuilder> {
  _$AddWorkerRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _capabilities;
  String? get capabilities => _$this._capabilities;
  set capabilities(String? capabilities) => _$this._capabilities = capabilities;

  AddWorkerRequestBuilder() {
    AddWorkerRequest._defaults(this);
  }

  AddWorkerRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _capabilities = $v.capabilities;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AddWorkerRequest other) {
    _$v = other as _$AddWorkerRequest;
  }

  @override
  void update(void Function(AddWorkerRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AddWorkerRequest build() => _build();

  _$AddWorkerRequest _build() {
    final _$result = _$v ??
        _$AddWorkerRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'AddWorkerRequest', 'id'),
          capabilities: capabilities,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
