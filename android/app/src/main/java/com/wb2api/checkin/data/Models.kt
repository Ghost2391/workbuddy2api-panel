package com.wb2api.checkin.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class Account(
    val uid: String,
    val nickname: String = "",
    val realm: String = "cn",
    val enterprise: Boolean = false,
    val file: String = "",
    @SerialName("last_checkin") val lastCheckin: CheckinResult? = null,
    @SerialName("has_credits") val hasCredits: Boolean = false,
    @SerialName("credits_remain") val creditsRemain: Long = 0,
    @SerialName("credits_total") val creditsTotal: Long = 0,
    @SerialName("credits_expiring") val creditsExpiring: Long = 0,
    @SerialName("credits_at") val creditsAt: String = "",
)

@Serializable
data class CheckinResult(
    val uid: String,
    val nickname: String = "",
    val status: String,
    val message: String = "",
    @SerialName("checked_at") val checkedAt: String = "",
    @SerialName("has_credits") val hasCredits: Boolean = false,
    @SerialName("credits_remain") val creditsRemain: Long = 0,
    @SerialName("credits_total") val creditsTotal: Long = 0,
    @SerialName("credits_expiring") val creditsExpiring: Long = 0,
    @SerialName("credits_at") val creditsAt: String = "",
    @SerialName("credits_error") val creditsError: String = "",
)

@Serializable
data class StartLoginResult(
    val state: String,
    val url: String,
    val realm: String = "cn",
)

@Serializable
data class PollLoginResult(
    val done: Boolean,
    val message: String = "",
    val account: Account? = null,
)

data class ImportFile(val name: String, val content: String?)

data class ImportSummary(
    val total: Int,
    val ok: Int,
    val failures: List<String>,
)
