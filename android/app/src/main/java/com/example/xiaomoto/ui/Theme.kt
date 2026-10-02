package com.example.xiaomoto.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.graphics.Color
import kotlinx.coroutines.flow.StateFlow

private val colors = darkColorScheme(
    primary = Color(0xFFFFC107),
    onPrimary = Color(0xFF12110E),
    background = Color(0xFF12110E),
    surface = Color(0xFF1C1A16),
    onBackground = Color(0xFFF5F1E8),
    onSurface = Color(0xFFF5F1E8),
)

@Composable
fun XiaoMotoTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = colors, content = content)
}

@Composable
fun <T> StateFlow<T>.collectAsStateCompat(): State<T> = collectAsState()
