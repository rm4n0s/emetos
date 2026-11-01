Projects of the Java pack (step 24b.6).

plain/   the project of the real-server tests (build tag emetos_jdtls): sources and no build file, which jdtls reads as an invisible
         project. It needs no network. The tests copy it into a temporary folder.
maven/   a Maven project with no dependencies, and gradle/ a Gradle project: roots for the tests with a fake server. jdtls cannot
         import them offline: m2e resolves the plugins of the Maven lifecycle (maven-resources-plugin, maven-compiler-plugin...) from
         Maven Central, and Gradle's import downloads Gradle itself, so the real-server tests do not use them (SPEC §10.34 "As built
         (step 24b.6)", decision 1013).
