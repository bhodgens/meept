//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/json_object.dart';
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'session_service.g.dart';

/// SessionService
///
/// Properties:
/// * [store] 
@BuiltValue()
abstract class SessionService implements Built<SessionService, SessionServiceBuilder> {
  @BuiltValueField(wireName: r'store')
  JsonObject? get store;

  SessionService._();

  factory SessionService([void updates(SessionServiceBuilder b)]) = _$SessionService;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(SessionServiceBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<SessionService> get serializer => _$SessionServiceSerializer();
}

class _$SessionServiceSerializer implements PrimitiveSerializer<SessionService> {
  @override
  final Iterable<Type> types = const [SessionService, _$SessionService];

  @override
  final String wireName = r'SessionService';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    SessionService object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.store != null) {
      yield r'store';
      yield serializers.serialize(
        object.store,
        specifiedType: const FullType(JsonObject),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    SessionService object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required SessionServiceBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'store':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(JsonObject),
          ) as JsonObject;
          result.store = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  SessionService deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = SessionServiceBuilder();
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

