package com.wb2api.checkin.data

interface EngineClient {
    fun listAccounts(): List<Account>
    fun importAccount(content: String): Account
    fun checkin(uid: String): CheckinResult
    fun refreshCredits(uid: String): Account
    fun deleteAccount(uid: String)
    fun exportAccount(uid: String): String
    fun startLogin(realm: String): StartLoginResult
    fun pollLogin(state: String): PollLoginResult
}
