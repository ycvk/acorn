package io.ycvk.acorn.core.io

import java.io.File
import java.io.FileOutputStream
import java.io.OutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption

/** Replaces [file] with what [write] puts in it at once: a crash leaves the old file or the new one. */
fun writeAtomically(file: File, write: (OutputStream) -> Unit) {
    val temporary = File(file.parentFile, "${file.name}.new")
    try {
        FileOutputStream(temporary).use { stream ->
            write(stream)
            stream.fd.sync()
        }
        Files.move(temporary.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
    } finally {
        Files.deleteIfExists(temporary.toPath())
    }
}
