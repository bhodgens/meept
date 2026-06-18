//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'claim_request.g.dart';

/// ClaimRequest
///
/// Properties:
/// * [workerId] 
/// * [capabilitiesCommaOmitempty] 
@BuiltValue()
abstract class ClaimRequest implements Built<ClaimRequest, ClaimRequestBuilder> {
  @BuiltValueField(wireName: r'worker_id')
  String get workerId;

  @BuiltValueField(wireName: r'capabilities,omitempty')
  String? get capabilitiesCommaOmitempty;

  ClaimRequest._();

  factory ClaimRequest([void updates(ClaimRequestBuilder b)]) = _$ClaimRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ClaimRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ClaimRequest> get serializer => _$ClaimRequestSerializer();
}

class _$ClaimRequestSerializer implements PrimitiveSerializer<ClaimRequest> {
  @override
  final Iterable<Type> types = const [ClaimRequest, _$ClaimRequest];

  @override
  final String wireName = r'ClaimRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ClaimRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'worker_id';
    yield serializers.serialize(
      object.workerId,
      specifiedType: const FullType(String),
    );
    if (object.capabilitiesCommaOmitempty != null) {
      yield r'capabilities,omitempty';
      yield serializers.serialize(
        object.capabilitiesCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ClaimRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ClaimRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'worker_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.workerId = valueDes;
          break;
        case r'capabilities,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.capabilitiesCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ClaimRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ClaimRequestBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

