# Keep Kotlinx Serialization models
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.AnnotationsKt
-keepclassmembers class kotlinx.serialization.json.** {
    *** Companion;
}
-keepclasseswithmembers class kotlinx.serialization.json.** {
    kotlinx.serialization.KSerializer serializer(...);
}
-keep,includedescriptorclasses class com.np1control.app.data.**$$serializer { *; }
-keepclassmembers class com.np1control.app.data.** {
    *** Companion;
}
-keepclasseswithmembers class com.np1control.app.data.** {
    kotlinx.serialization.KSerializer serializer(...);
}
