// Root build file for the muesli Android workspace (issue #768: View Notes
// on Android): the preparatory `:notes` library (Tasks 1-5) plus the
// runnable `:app` host application (Task 6) that authenticates and
// navigates into it.
plugins {
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.compose.compiler) apply false
}
