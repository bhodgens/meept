//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'audit_request.g.dart';

/// AuditRequest
///
/// Properties:
/// * [limitCommaOmitempty] 
@BuiltValue()
abstract class AuditRequest implements Built<AuditRequest, AuditRequestBuilder> {
  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

  AuditRequest._();

  factory AuditRequest([void updates(AuditRequestBuilder b)]) = _$AuditRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(AuditRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<AuditRequest> get serializer => _$AuditRequestSerializer();
}

class _$AuditRequestSerializer implements PrimitiveSerializer<AuditRequest> {
  @override
  final Iterable<Type> types = const [AuditRequest, _$AuditRequest];

  @override
  final String wireName = r'AuditRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    AuditRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.limitCommaOmitempty != null) {
      yield r'limit,omitempty';
      yield serializers.serialize(
        object.limitCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    AuditRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required AuditRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'limit,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limitCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  AuditRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = AuditRequestBuilder();
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

