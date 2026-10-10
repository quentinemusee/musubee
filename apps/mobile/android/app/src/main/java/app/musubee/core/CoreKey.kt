// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.AtomicFile
import java.io.File
import java.io.IOException
import java.security.KeyStore
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * The core's master key on Android (docs/ADR/0019): 32 random bytes that
 * seal the accounts' sessions in the core's database.
 *
 * The key is kept in [file], encrypted with an AES-256-GCM key of the Android
 * Keystore named [alias]. That key never leaves the Keystore (in secure
 * hardware when the device has some), so a copy of the app's files without
 * the device does not give the sessions.
 *
 * File format: one version byte (1), one byte with the length of the IV, the
 * IV, then the ciphertext with its 16-byte tag.
 */
class CoreKey(private val file: File, private val alias: String) {
    /**
     * Returns the master key, creating it on first use. Throws if the file
     * exists but its Keystore key is gone (the app's files were copied to
     * another device, or the Keystore was cleared): the sessions cannot be
     * read again, and a new key would not open the database either.
     */
    @Synchronized
    fun load(): ByteArray {
        val atomic = AtomicFile(file)
        if (!file.exists()) {
            val key = ByteArray(KEY_SIZE).also { SecureRandom().nextBytes(it) }
            val sealed = wrap(key)
            val out = atomic.startWrite()
            try {
                out.write(sealed)
                atomic.finishWrite(out)
            } catch (e: IOException) {
                atomic.failWrite(out)
                throw e
            }
            return key
        }
        return unwrap(atomic.readFully())
    }

    private fun keystore(): KeyStore = KeyStore.getInstance(KEYSTORE).apply { load(null) }

    private fun wrappingKey(create: Boolean): SecretKey {
        (keystore().getKey(alias, null) as SecretKey?)?.let { return it }
        check(create) { "the Keystore key of the core's master key is gone" }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(KEY_SIZE * 8)
                .build(),
        )
        return generator.generateKey()
    }

    private fun wrap(key: ByteArray): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        // The Keystore chooses the IV: it refuses one from the caller.
        cipher.init(Cipher.ENCRYPT_MODE, wrappingKey(create = true))
        cipher.updateAAD(AAD)
        val iv = cipher.iv
        val ciphertext = cipher.doFinal(key)
        return byteArrayOf(VERSION, iv.size.toByte()) + iv + ciphertext
    }

    private fun unwrap(sealed: ByteArray): ByteArray {
        check(sealed.size > 2 && sealed[0] == VERSION) { "the core's key file is not in a known format" }
        val ivSize = sealed[1].toInt()
        check(ivSize in 1..sealed.size - 2 - TAG_BITS / 8) { "the core's key file is malformed" }
        val iv = sealed.copyOfRange(2, 2 + ivSize)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, wrappingKey(create = false), GCMParameterSpec(TAG_BITS, iv))
        cipher.updateAAD(AAD)
        val key = cipher.doFinal(sealed, 2 + ivSize, sealed.size - 2 - ivSize)
        check(key.size == KEY_SIZE) { "the core's key file holds a key of the wrong size" }
        return key
    }

    companion object {
        private const val KEYSTORE = "AndroidKeyStore"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val KEY_SIZE = 32
        private const val TAG_BITS = 128
        private const val VERSION: Byte = 1
        private val AAD = "app.musubee.core master key v1".toByteArray(Charsets.UTF_8)

        /** The app's key: outside the core's data directory, never backed up. */
        fun forApp(context: Context) = CoreKey(File(context.noBackupFilesDir, "core-key"), "app.musubee.core.key-wrap")
    }
}
