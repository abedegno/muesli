// Root build file for the muesli Android workspace (issue #768: View Notes on
// Android). Deliberately minimal: no application module exists yet (Task 6,
// out of scope for this slice) -- only the preparatory `:notes` library.
plugins {
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.compose.compiler) apply false
}
