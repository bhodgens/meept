//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'check_request.g.dart';

/// CheckRequest
///
/// Properties:
/// * [action] 
/// * [resource] 
@BuiltValue()
abstract class CheckRequest implements Built<CheckRequest, CheckRequestBuilder> {
  @BuiltValueField(wireName: r'action')
  String get action;

  @BuiltValueField(wireName: r'resource')
  String get resource;

  CheckRequest._();

  factory CheckRequest([void updates(CheckRequestBuilder b)]) = _$CheckRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CheckRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CheckRequest> get serializer => _$CheckRequestSerializer();
}

class _$CheckRequestSerializer implements PrimitiveSerializer<CheckRequest> {
  @override
  final Iterable<Type> types = const [CheckRequest, _$CheckRequest];

  @override
  final String wireName = r'CheckRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CheckRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'action';
    yield serializers.serialize(
      object.action,
      specifiedType: const FullType(String),
    );
    yield r'resource';
    yield serializers.serialize(
      object.resource,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    CheckRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CheckRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'action':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.action = valueDes;
          break;
        case r'resource':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.resource = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CheckRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CheckRequestBuilder();
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

