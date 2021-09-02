require_relative 'portal/auth_client'
require_relative 'certificate_helper'
require_relative 'profile'
require_relative 'log'
require 'optparse'

begin
  options = {}
  OptionParser.new do |opt|
    opt.on('--subcommand SUBCOMMAND') { |o| options[:subcommand] = o }
    opt.on('--bundle_id BUNDLE_ID') { |o| options[:bundle_id] = o }
    opt.on('--certificate CERTIFICATE') { |o| options[:certificate] = o }
    opt.on('--profile_name PROFILE_NAME') { |o| options[:profile_name] = o }
  end.parse!

  Log.verbose = true

  Portal::AuthClient.login(apple_id, password)
  Log.info('logged in')

  case options[:subcommand]
  when 'list_dev_certs'
    client = CertificateHelper.new
    certificates = client.list_dev_certs
    result = { data: certificates }
    puts result.to_json.to_s
  when 'create_profile'
    create_profile(options[:bundle_id], options[:certificate], options[:profile_name])
  end
rescue => e
  result = { error: "Error: #{e} Stacktrace: #{e.backtrace.join("\n")}" }
  puts result.to_json.to_s

  exit 1
end
