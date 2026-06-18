// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'update_event_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$UpdateEventRequest extends UpdateEventRequest {
  @override
  final String id;
  @override
  final String? summaryCommaOmitempty;
  @override
  final String? descriptionCommaOmitempty;
  @override
  final String? locationCommaOmitempty;
  @override
  final String? startCommaOmitempty;
  @override
  final String? endCommaOmitempty;

  factory _$UpdateEventRequest(
          [void Function(UpdateEventRequestBuilder)? updates]) =>
      (UpdateEventRequestBuilder()..update(updates))._build();

  _$UpdateEventRequest._(
      {required this.id,
      this.summaryCommaOmitempty,
      this.descriptionCommaOmitempty,
      this.locationCommaOmitempty,
      this.startCommaOmitempty,
      this.endCommaOmitempty})
      : super._();
  @override
  UpdateEventRequest rebuild(
          void Function(UpdateEventRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  UpdateEventRequestBuilder toBuilder() =>
      UpdateEventRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is UpdateEventRequest &&
        id == other.id &&
        summaryCommaOmitempty == other.summaryCommaOmitempty &&
        descriptionCommaOmitempty == other.descriptionCommaOmitempty &&
        locationCommaOmitempty == other.locationCommaOmitempty &&
        startCommaOmitempty == other.startCommaOmitempty &&
        endCommaOmitempty == other.endCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, summaryCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, descriptionCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, locationCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, startCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, endCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UpdateEventRequest')
          ..add('id', id)
          ..add('summaryCommaOmitempty', summaryCommaOmitempty)
          ..add('descriptionCommaOmitempty', descriptionCommaOmitempty)
          ..add('locationCommaOmitempty', locationCommaOmitempty)
          ..add('startCommaOmitempty', startCommaOmitempty)
          ..add('endCommaOmitempty', endCommaOmitempty))
        .toString();
  }
}

class UpdateEventRequestBuilder
    implements Builder<UpdateEventRequest, UpdateEventRequestBuilder> {
  _$UpdateEventRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _summaryCommaOmitempty;
  String? get summaryCommaOmitempty => _$this._summaryCommaOmitempty;
  set summaryCommaOmitempty(String? summaryCommaOmitempty) =>
      _$this._summaryCommaOmitempty = summaryCommaOmitempty;

  String? _descriptionCommaOmitempty;
  String? get descriptionCommaOmitempty => _$this._descriptionCommaOmitempty;
  set descriptionCommaOmitempty(String? descriptionCommaOmitempty) =>
      _$this._descriptionCommaOmitempty = descriptionCommaOmitempty;

  String? _locationCommaOmitempty;
  String? get locationCommaOmitempty => _$this._locationCommaOmitempty;
  set locationCommaOmitempty(String? locationCommaOmitempty) =>
      _$this._locationCommaOmitempty = locationCommaOmitempty;

  String? _startCommaOmitempty;
  String? get startCommaOmitempty => _$this._startCommaOmitempty;
  set startCommaOmitempty(String? startCommaOmitempty) =>
      _$this._startCommaOmitempty = startCommaOmitempty;

  String? _endCommaOmitempty;
  String? get endCommaOmitempty => _$this._endCommaOmitempty;
  set endCommaOmitempty(String? endCommaOmitempty) =>
      _$this._endCommaOmitempty = endCommaOmitempty;

  UpdateEventRequestBuilder() {
    UpdateEventRequest._defaults(this);
  }

  UpdateEventRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _summaryCommaOmitempty = $v.summaryCommaOmitempty;
      _descriptionCommaOmitempty = $v.descriptionCommaOmitempty;
      _locationCommaOmitempty = $v.locationCommaOmitempty;
      _startCommaOmitempty = $v.startCommaOmitempty;
      _endCommaOmitempty = $v.endCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(UpdateEventRequest other) {
    _$v = other as _$UpdateEventRequest;
  }

  @override
  void update(void Function(UpdateEventRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  UpdateEventRequest build() => _build();

  _$UpdateEventRequest _build() {
    final _$result = _$v ??
        _$UpdateEventRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'UpdateEventRequest', 'id'),
          summaryCommaOmitempty: summaryCommaOmitempty,
          descriptionCommaOmitempty: descriptionCommaOmitempty,
          locationCommaOmitempty: locationCommaOmitempty,
          startCommaOmitempty: startCommaOmitempty,
          endCommaOmitempty: endCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
